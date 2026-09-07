#include <chrono>
#include <fcntl.h>
#include <filesystem>
#include <hyprland/src/Compositor.hpp>
#include <hyprland/src/desktop/state/FocusState.hpp>
#include <hyprland/src/desktop/state/WindowState.hpp>
#include <hyprland/src/devices/IKeyboard.hpp>
#include <hyprland/src/devices/IPointer.hpp>
#include <hyprland/src/managers/SeatManager.hpp>
#include <hyprland/src/managers/input/InputManager.hpp>
#include <hyprland/src/plugins/PluginAPI.hpp>
#include <hyprland/src/pointer/PointerManager.hpp>
#include <map>
#include <nlohmann/json.hpp>
#include <set>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

using json = nlohmann::json;
using Clock = std::chrono::steady_clock;
static HANDLE handle;
static int serverFD = -1;
static wl_event_source *serverEvent = nullptr, *timerEvent = nullptr;
static std::string socketPath;
struct Lease {
  PHLWINDOWREF window;
  WP<CWLSurfaceResource> surface;
  Clock::time_point until;
  std::set<uint32_t> keys, buttons;
};
static std::map<std::string, Lease> leases;
struct Peer {
  int fd;
  wl_event_source *event;
  std::string data;
};
static std::set<Peer *> peers;
static uint32_t millis() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
             Clock::now().time_since_epoch())
      .count();
}
static std::string revision(PHLWINDOW w) {
  auto b = w->getWindowMainSurfaceBox();
  return std::format("{},{},{},{}", (int)b.x, (int)b.y, (int)b.w, (int)b.h);
}
static PHLWINDOW findWindow(const std::string &id) {
  for (auto &w : Desktop::windowState()->windows())
    if (std::format("{:x}", w->m_stableID) == id && w->m_isMapped)
      return w;
  return nullptr;
}
static bool locked() {
  return !g_pCompositor->m_sessionActive ||
         g_pSessionLockManager->isSessionLocked();
}
static void release(Lease &l) {
  auto surf = l.surface.lock();
  if (surf) {
    if (g_pSeatManager->m_state.keyboardFocus.lock() == surf) {
      for (auto k : l.keys)
        g_pSeatManager->sendKeyboardKey(millis(), k,
                                        WL_KEYBOARD_KEY_STATE_RELEASED);
      if (!l.keys.empty())
        g_pSeatManager->sendKeyboardMods(0, 0, 0, 0);
    }
    if (g_pSeatManager->m_state.pointerFocus.lock() == surf) {
      for (auto b : l.buttons)
        g_pSeatManager->sendPointerButton(millis(), b,
                                          WL_POINTER_BUTTON_STATE_RELEASED);
      g_pSeatManager->sendPointerFrame();
    }
  }
  l.keys.clear();
  l.buttons.clear();
}
static void clear() {
  for (auto &[id, l] : leases)
    release(l);
  leases.clear();
}
static void expire() {
  for (auto it = leases.begin(); it != leases.end();) {
    if (locked() || Clock::now() >= it->second.until || !it->second.window ||
        !it->second.window->m_isMapped ||
        it->second.surface.lock() != it->second.window->resource()) {
      release(it->second);
      it = leases.erase(it);
    } else
      ++it;
  }
}
static json execute(const json &q) {
  expire();
  auto op = q.value("op", "");
  if (op == "status")
    return {{"ok", true},
            {"backend", "hyprland-compositor"},
            {"version", 1},
            {"leases", leases.size()},
            {"locked", locked()}};
  if (op == "clear") {
    clear();
    return {{"ok", true}};
  }
  auto token = q.value("token", "");
  if (op == "revoke") {
    auto it = leases.find(token);
    if (it != leases.end()) {
      release(it->second);
      leases.erase(it);
    }
    return {{"ok", true}};
  }
  if (op == "authorize") {
    if (locked())
      throw std::runtime_error("session_locked");
    if (token.size() < 20 || token.size() > 128 || leases.size() >= 64)
      throw std::runtime_error("invalid_lease");
    auto w = findWindow(q.value("window", ""));
    if (!w || w->m_isX11)
      throw std::runtime_error("native_window_required");
    if (w->m_class == "org.quickshell" || w->m_class == "computer-use")
      throw std::runtime_error("trusted_ui_denied");
    int ms = q.value("milliseconds", 0);
    if (ms < 1 || ms > 3600000)
      throw std::runtime_error("invalid_duration");
    if (leases.contains(token))
      release(leases.at(token));
    leases[token] = {
        w, w->resource(), Clock::now() + std::chrono::milliseconds(ms), {}, {}};
    return {{"ok", true}};
  }
  auto it = leases.find(token);
  if (it == leases.end())
    throw std::runtime_error("lease_expired_or_revoked");
  auto &l = it->second;
  auto w = l.window.lock();
  if (op == "release") {
    release(l);
    return {{"ok", true}};
  }
  if (!w || !w->m_isMapped || w->resource() != l.surface.lock())
    throw std::runtime_error("target_closed");
  if (!w->visible() || !w->acceptsInput())
    throw std::runtime_error("target_not_visible");
  if (q.value("revision", "") != revision(w))
    throw std::runtime_error("stale_geometry");
  auto surf = w->resource();
  if (!surf)
    throw std::runtime_error("target_surface_missing");
  // Direct seat delivery runs on the compositor event loop. Never a global
  // virtual-input fallback. Root toplevel only: transient toplevels need
  // independent grants; popup interaction is not claimed.
  if (op == "focus") {
    Desktop::focusState()->fullWindowFocus(w, Desktop::FOCUS_REASON_OTHER,
                                           surf);
  } else if (op == "pointer") {
    double x = q.value("x", -1.0), y = q.value("y", -1.0);
    auto box = w->getWindowMainSurfaceBox();
    if (!std::isfinite(x) || !std::isfinite(y) || x < 0 || y < 0 ||
        x >= box.w || y >= box.h)
      throw std::runtime_error("outside_window");
    Desktop::focusState()->fullWindowFocus(w, Desktop::FOCUS_REASON_OTHER,
                                           surf);
    // A virtual pointer supplies seat capability, not global event injection.
    // Local virtual devices are trusted; never borrow a physical device here.
    SP<IPointer> pointer;
    for (const auto &p : g_pInputManager->m_pointers)
      if (p->isVirtual()) {
        pointer = p;
        break;
      }
    if (!pointer)
      throw std::runtime_error("pointer_not_ready");
    g_pSeatManager->setMouse(pointer);
    Pointer::mgr()->warpTo({box.x + x, box.y + y});
    g_pSeatManager->setPointerFocus(surf, {x, y});
    if (g_pSeatManager->m_state.pointerFocus.lock() != surf)
      throw std::runtime_error("pointer_focus_refused");
    g_pSeatManager->sendPointerMotion(millis(), {x, y});
    auto state = q.value("button_state", "");
    uint32_t button = q.value("button", 272u);
    if (state == "down" || state == "up") {
      if (button < 272 || button > 274)
        throw std::runtime_error("invalid_button");
      g_pSeatManager->sendPointerButton(millis(), button,
                                        state == "down"
                                            ? WL_POINTER_BUTTON_STATE_PRESSED
                                            : WL_POINTER_BUTTON_STATE_RELEASED);
      if (state == "down")
        l.buttons.insert(button);
      else
        l.buttons.erase(button);
    }
    if (q.contains("scroll")) {
      double delta = q["scroll"];
      if (!std::isfinite(delta) || std::abs(delta) > 1200)
        throw std::runtime_error("invalid_scroll");
      g_pSeatManager->sendPointerAxis(
          millis(), WL_POINTER_AXIS_VERTICAL_SCROLL, delta, 0,
          (int)(delta * 12), WL_POINTER_AXIS_SOURCE_WHEEL,
          WL_POINTER_AXIS_RELATIVE_DIRECTION_IDENTICAL);
    }
    g_pSeatManager->sendPointerFrame();
  } else if (op == "key") {
    if (!g_pSeatManager->m_keyboard)
      throw std::runtime_error("keyboard_not_ready");
    uint32_t key = q.value("key", 0u), mods = q.value("mods", 0u);
    if (key > 255 || mods > 255)
      throw std::runtime_error("invalid_key");
    Desktop::focusState()->fullWindowFocus(w, Desktop::FOCUS_REASON_OTHER,
                                           surf);
    g_pSeatManager->setKeyboardFocus(surf);
    if (g_pSeatManager->m_state.keyboardFocus.lock() != surf)
      throw std::runtime_error("keyboard_focus_refused");
    g_pSeatManager->sendKeyboardMods(mods, 0, 0, 0);
    bool down = q.value("down", true);
    g_pSeatManager->sendKeyboardKey(millis(), key,
                                    down ? WL_KEYBOARD_KEY_STATE_PRESSED
                                         : WL_KEYBOARD_KEY_STATE_RELEASED);
    if (down)
      l.keys.insert(key);
    else {
      l.keys.erase(key);
      if (l.keys.empty())
        g_pSeatManager->sendKeyboardMods(0, 0, 0, 0);
    }
  } else if (op == "release") {
    release(l);
  } else
    throw std::runtime_error("unknown_operation");
  return {{"ok", true}, {"revision", revision(w)}};
}
static void closePeer(Peer *p) {
  wl_event_source_remove(p->event);
  close(p->fd);
  peers.erase(p);
  delete p;
}
static int readPeer(int fd, uint32_t mask, void *data) {
  auto *p = (Peer *)data;
  if (mask & (WL_EVENT_HANGUP | WL_EVENT_ERROR)) {
    closePeer(p);
    return 0;
  }
  char b[4096];
  auto n = read(fd, b, sizeof(b));
  if (n <= 0) {
    if (n == 0)
      closePeer(p);
    return 0;
  }
  p->data.append(b, n);
  if (p->data.size() > 16384) {
    closePeer(p);
    return 0;
  }
  if (p->data.find('\n') == std::string::npos)
    return 0;
  json out;
  try {
    out = execute(json::parse(p->data));
  } catch (const std::exception &e) {
    out = {{"ok", false}, {"error", e.what()}};
  }
  auto text = out.dump() + "\n";
  send(fd, text.data(), text.size(), MSG_NOSIGNAL);
  closePeer(p);
  return 0;
}
static int acceptPeer(int fd, uint32_t, void *) {
  int c = accept4(fd, nullptr, nullptr, SOCK_NONBLOCK | SOCK_CLOEXEC);
  if (c < 0)
    return 0;
  ucred cred{};
  socklen_t len = sizeof(cred);
  if (getsockopt(c, SOL_SOCKET, SO_PEERCRED, &cred, &len) ||
      cred.uid != getuid() || peers.size() >= 32) {
    close(c);
    return 0;
  }
  auto *p = new Peer{c, nullptr, {}};
  p->event = wl_event_loop_add_fd(g_pCompositor->m_wlEventLoop, c,
                                  WL_EVENT_READABLE, readPeer, p);
  peers.insert(p);
  return 0;
}
static int tick(void *) {
  expire();
  wl_event_source_timer_update(timerEvent, 50);
  return 0;
}
APICALL EXPORT std::string PLUGIN_API_VERSION() { return HYPRLAND_API_VERSION; }
APICALL EXPORT PLUGIN_DESCRIPTION_INFO PLUGIN_INIT(HANDLE h) {
  handle = h;
  if (HyprlandAPI::getHyprlandVersion(h).hash != GIT_COMMIT_HASH)
    throw std::runtime_error(
        "Rebuild computer-use-guard against this exact Hyprland version");
  std::string dir = std::string(getenv("XDG_RUNTIME_DIR")) + "/computer-use";
  std::filesystem::create_directories(dir);
  chmod(dir.c_str(), 0700);
  socketPath = dir + "/guard.sock";
  serverFD = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
  sockaddr_un addr{};
  addr.sun_family = AF_UNIX;
  if (socketPath.size() >= sizeof(addr.sun_path))
    throw std::runtime_error("socket path too long");
  strcpy(addr.sun_path, socketPath.c_str());
  unlink(socketPath.c_str());
  if (bind(serverFD, (sockaddr *)&addr, sizeof(addr)) || listen(serverFD, 16))
    throw std::runtime_error("guard socket failed");
  chmod(socketPath.c_str(), 0600);
  serverEvent = wl_event_loop_add_fd(g_pCompositor->m_wlEventLoop, serverFD,
                                     WL_EVENT_READABLE, acceptPeer, nullptr);
  timerEvent =
      wl_event_loop_add_timer(g_pCompositor->m_wlEventLoop, tick, nullptr);
  wl_event_source_timer_update(timerEvent, 50);
  return {"computer-use-guard",
          "Window-scoped seat delivery for the Computer Use broker",
          "Computer Use", "0.1.0"};
}
APICALL EXPORT void PLUGIN_EXIT() {
  clear();
  while (!peers.empty())
    closePeer(*peers.begin());
  if (timerEvent)
    wl_event_source_remove(timerEvent);
  if (serverEvent)
    wl_event_source_remove(serverEvent);
  if (serverFD >= 0)
    close(serverFD);
  unlink(socketPath.c_str());
}
