#include <cerrno>
#include <chrono>
#include <cstdio>
#include <cstdlib>
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
#include <hyprland/src/protocols/InputCapture.hpp>
#include <hyprland/src/protocols/InputMethodV2.hpp>
#include <hyprland/src/protocols/core/Compositor.hpp>
#include <hyprland/src/protocols/core/DataDevice.hpp>
#include <hyprland/src/protocols/core/Seat.hpp>
#include <map>
#include <nlohmann/json.hpp>
#include <set>
#include <sys/file.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

using json = nlohmann::json;
using Clock = std::chrono::steady_clock;
static HANDLE handle;
static int serverFD = -1, lockFD = -1;
static wl_event_source *serverEvent = nullptr, *timerEvent = nullptr;
static std::string socketPath;
struct Lease {
  PHLWINDOWREF window;
  WP<CWLSurfaceResource> surface;
  Clock::time_point until;
};
static std::map<std::string, Lease> leases;
struct Peer {
  int fd;
  pid_t pid;
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
#include "input_transaction.hpp"
#include "surface_tree.hpp"
#include "text_keyboard.hpp"
#include "text_transaction.hpp"
static const std::string guardInstance = surfaceEpoch();
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
#include "independent_seat.hpp"
#endif

static void clear() {
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
  if (independentSeat)
    independentSeat->reset();
#endif
  leases.clear();
}
static void expire() {
  for (auto it = leases.begin(); it != leases.end();) {
    if (locked() || Clock::now() >= it->second.until || !it->second.window ||
        !it->second.window->m_isMapped ||
        it->second.surface.lock() != it->second.window->resource()) {
      it = leases.erase(it);
    } else
      ++it;
  }
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
  if (independentSeat)
    independentSeat->tick();
#endif
}
static json execute(const json &q, pid_t owner) {
  expire();
  auto op = q.value("op", "");
  const auto expectedInstance = q.value("instance", "");
  if (!expectedInstance.empty() && expectedInstance != guardInstance)
    throw std::runtime_error("compositor_instance_changed_restart_broker");
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
  if (op != "status" && expectedInstance != guardInstance)
    throw std::runtime_error("compositor_instance_required");
#endif
  if (op == "input_modes") {
    json modes = json::object();
    for (auto &window : Desktop::windowState()->windows()) {
      if (!window->m_isMapped || window->m_isX11)
        continue;
      bool seat = false;
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
      seat = independentSeat->supports(window->resource());
#endif
      modes[std::format("{:x}", window->m_stableID)] =
          seat ? "Seat" : "Fallback";
    }
    return {{"ok", true}, {"modes", modes}};
  }
  if (op == "status")
    return {{"ok", true},
            {"backend", "hyprland-compositor"},
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
            {"version", 3},
#else
            {"version", 2},
#endif
            {"focus_preserving", true},
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
            {"independent_seat", true},
            {"automatic_fallback", true},
            {"seat_resources", independentSeat->clients()},
            {"refused_popup_grabs", independentSeat->deniedGrabs()},
#endif
            {"unicode_text", true},
            {"text_chunk_runes", TEXT_CHUNK_RUNES},
            {"surface_tree_version", 1},
            {"input_faulted", inputFaulted},
            {"leases", leases.size()},
            {"locked", locked()}};
  if (op == "window_state") {
    auto w = findWindow(q.value("window", ""));
    if (!w || w->m_isX11)
      throw std::runtime_error("native_window_required");
    return {{"ok", true}, {"state", windowState(w)}};
  }
  if (op == "clear") {
    clear();
    return {{"ok", true}};
  }
  auto token = q.value("token", "");
  if (op == "revoke") {
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    independentSeat->revoke(token);
#endif
    auto it = leases.find(token);
    if (it != leases.end()) {
      leases.erase(it);
    }
    return {{"ok", true}};
  }
  if (op == "authorize") {
    if (inputFaulted)
      throw std::runtime_error("input_restore_failed_reload_plugin");
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
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    independentSeat->revoke(
        token); // renewed/replaced leases cannot retain old serials
#endif
    leases[token] = {w, w->resource(),
                     Clock::now() + std::chrono::milliseconds(ms)};
    return {{"ok", true}};
  }
  auto it = leases.find(token);
  if (it == leases.end())
    throw std::runtime_error("lease_expired_or_revoked");
  auto &l = it->second;
  auto w = l.window.lock();
  if (op == "release") {
    return {{"ok", true}}; // no synthetic state survives a transaction
  }
  if (!w || !w->m_isMapped || w->resource() != l.surface.lock())
    throw std::runtime_error("target_closed");
  if (!w->visible() || !w->acceptsInput())
    throw std::runtime_error("target_not_visible");
  if (q.value("revision", "") != revision(w))
    throw std::runtime_error("stale_geometry");
  if (inputFaulted)
    throw std::runtime_error("input_restore_failed_reload_plugin");
  const auto nodes = windowSurfaceTree(w);
  const auto selected = selectWindowSurface(w, nodes, q);
  auto surf = selected.surface;
  bool useSeat = false;
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
  useSeat = independentSeat->supports(l.surface.lock());
#endif
  const auto inputMode = useSeat ? "seat" : "fallback";
  // Only this explicit action requests desktop activation. Default input
  // prefers its own seat and otherwise borrows/restores native focus.
  // Application-created toplevels/activation requests are not suppressed.
  if (op == "focus") {
    if (!q.value("surface_id", "").empty())
      throw std::runtime_error("surface_focus_action_unsupported");
    requireIdleInput();
    Desktop::focusState()->fullWindowFocus(w, Desktop::FOCUS_REASON_OTHER,
                                           surf);
  } else if (op == "key_transaction") {
    uint32_t key = q.value("key", 0u), mods = q.value("mods", 0u);
    if (key > 255 || mods > 255)
      throw std::runtime_error("invalid_key");
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    if (useSeat) {
      independentSeat->begin(token, l.surface.lock(), surf, {}, true,
                             !q.value("surface_id", "").empty());
      independentSeat->key(key, mods);
    } else
#endif
    {
      auto agent = agentKeyboard(owner);
      InputTransaction transaction(surf, false);
      transaction.borrowKeyboard(agent);
      transaction.key(key, mods);
      transaction.finish();
    }
  } else if (op == "text_transaction") {
    const auto values = q.value("scalars", json::array());
    if (!values.is_array() || values.empty() ||
        values.size() > TEXT_CHUNK_RUNES)
      throw std::runtime_error("invalid_text_chunk_size");
    std::vector<uint32_t> scalars;
    for (const auto &value : values) {
      if (!value.is_number_integer() || value < 0 || value > 0x10ffff)
        throw std::runtime_error("invalid_text_scalar");
      const auto scalar = value.get<uint32_t>();
      textKeysym(
          scalar); // reject controls/surrogates before allocating the map
      scalars.push_back(scalar);
    }
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    if (useSeat) {
      independentSeat->begin(token, l.surface.lock(), surf, {}, true,
                             !q.value("surface_id", "").empty());
      independentSeat->text(scalars);
    } else
#endif
    {
      auto agent = agentKeyboard(owner);
      textTransaction(surf, agent, scalars, [](const std::string &map) {
        return makeShared<TextKeyboard>(map);
      });
    }
    return {{"ok", true},
            {"revision", revision(w)},
            {"input_mode", inputMode},
            {"completed_characters", scalars.size()}};
  } else if (op == "pointer_transaction") {
    const auto kind = q.value("kind", "");
    if (kind != "move" && kind != "click" && kind != "scroll" && kind != "drag")
      throw std::runtime_error("invalid_pointer_transaction");
    const auto box = w->getWindowMainSurfaceBox();
    const auto size =
        q.value("surface_id", "").empty() ? box.size() : selected.size;
    const auto valid = [&](double x, double y) {
      return surfacePointInside({x, y}, size);
    };
    double x = q.value("x", -1.0), y = q.value("y", -1.0);
    double toX = q.value("to_x", x), toY = q.value("to_y", y);
    uint32_t button = q.value("button", 272u);
    double delta = q.value("scroll", 0.0);
    if (!valid(x, y) || !valid(toX, toY))
      throw std::runtime_error("outside_window");
    if (button < 272 || button > 274)
      throw std::runtime_error("invalid_button");
    if (!std::isfinite(delta) || std::abs(delta) > 1200)
      throw std::runtime_error("invalid_scroll");
    if (q.value("duration_ms", 0) != 0)
      throw std::runtime_error("timed_drag_unsupported_use_duration_zero");
    // Validate everything above before sending even a pointer enter.
    const auto path = planSurfacePath(
        nodes, size, {x, y}, {toX, toY}, kind == "drag", [&](Vector2D point) {
          return hitSurfaceTree(
              nodes, selected, point, [](auto surface, Vector2D local) {
                return surface->m_current.effectiveInputRegion().containsPoint(
                    local);
              });
        });
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    if (useSeat) {
      independentSeat->begin(token, l.surface.lock(), path.surface,
                             path.points.front());
      independentSeat->motion(path.points.front());
      if (kind == "click" || kind == "drag") {
        independentSeat->button(button, true);
        if (kind == "drag")
          for (size_t i = 1; i < path.points.size(); ++i)
            independentSeat->motion(path.points[i]);
        independentSeat->button(button, false);
      } else if (kind == "scroll")
        independentSeat->scroll(delta);
    } else
#endif
    {
      InputTransaction transaction(path.surface, true);
      transaction.borrowPointer(path.points.front());
      transaction.motion(path.points.front());
      if (kind == "click" || kind == "drag") {
        transaction.button(button, true);
        if (kind == "drag") {
          // Bounded burst, no sleep/dispatch with a button held or focus
          // borrowed.
          for (size_t i = 1; i < path.points.size(); ++i)
            transaction.motion(path.points[i]);
        }
        transaction.button(button, false);
      } else if (kind == "scroll") {
        g_pSeatManager->sendPointerAxis(
            millis(), WL_POINTER_AXIS_VERTICAL_SCROLL, delta, 0,
            (int)(delta * 12), WL_POINTER_AXIS_SOURCE_WHEEL,
            WL_POINTER_AXIS_RELATIVE_DIRECTION_IDENTICAL);
        g_pSeatManager->sendPointerFrame();
      }
      transaction.finish();
    }
  } else
    throw std::runtime_error("unknown_operation_update_broker_and_plugin");
  return {{"ok", true}, {"revision", revision(w)}, {"input_mode", inputMode}};
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
  bool textRequest = false;
  try {
    const auto q = json::parse(p->data);
    textRequest =
        q.is_object() && q.contains("op") && q["op"] == "text_transaction";
    out = execute(q, p->pid);
  } catch (const TextFailure &e) {
    out = {{"ok", false},
           {"error", e.what()},
           {"completed_characters", e.completed}};
  } catch (const std::exception &e) {
    out = {{"ok", false}, {"error", e.what()}};
    if (textRequest)
      out["completed_characters"] = 0;
  }
  out["instance"] = guardInstance;
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
  auto *p = new Peer{c, cred.pid, nullptr, {}};
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
static void cleanupGuard();
APICALL EXPORT std::string PLUGIN_API_VERSION() { return HYPRLAND_API_VERSION; }
APICALL EXPORT PLUGIN_DESCRIPTION_INFO PLUGIN_INIT(HANDLE h) {
  handle = h;
  if (HyprlandAPI::getHyprlandVersion(h).hash != GIT_COMMIT_HASH)
    throw std::runtime_error(
        "computer-use-guard was built for a different Hyprland commit; rebuild "
        "it against the exact running Hyprland version");
  try {
    const char *runtime = getenv("XDG_RUNTIME_DIR");
    if (!runtime || !*runtime) {
      std::fprintf(stderr,
                   "computer-use-guard: XDG_RUNTIME_DIR is required; refusing "
                   "plugin initialization\n");
      throw std::runtime_error("XDG_RUNTIME_DIR is required");
    }
    std::string dir = std::string(runtime) + "/computer-use";
    std::filesystem::create_directories(dir);
    if (chmod(dir.c_str(), 0700) != 0)
      throw std::runtime_error("guard runtime directory chmod failed");
    socketPath = dir + "/guard.sock";
    const std::string lockPath = socketPath + ".lock";
    const int candidateLock =
        open(lockPath.c_str(), O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (candidateLock < 0)
      throw std::runtime_error("guard socket lock creation failed");
    if (flock(candidateLock, LOCK_EX | LOCK_NB) != 0) {
      close(candidateLock);
      throw std::runtime_error("guard socket already owned by another plugin");
    }
    lockFD = candidateLock;
    serverFD = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
    if (serverFD < 0)
      throw std::runtime_error("guard socket creation failed");
    sockaddr_un addr{};
    addr.sun_family = AF_UNIX;
    if (socketPath.size() >= sizeof(addr.sun_path))
      throw std::runtime_error("socket path too long");
    strcpy(addr.sun_path, socketPath.c_str());
    // The adjacent flock survives a busy event loop and is released by the
    // kernel on compositor exit. Only its owner may reclaim a stale pathname.
    if (unlink(socketPath.c_str()) != 0 && errno != ENOENT)
      throw std::runtime_error("stale guard socket removal failed");
    if (bind(serverFD, (sockaddr *)&addr, sizeof(addr)) || listen(serverFD, 16))
      throw std::runtime_error("guard socket failed");
    if (chmod(socketPath.c_str(), 0600) != 0)
      throw std::runtime_error("guard socket chmod failed");
    serverEvent = wl_event_loop_add_fd(g_pCompositor->m_wlEventLoop, serverFD,
                                       WL_EVENT_READABLE, acceptPeer, nullptr);
    timerEvent =
        wl_event_loop_add_timer(g_pCompositor->m_wlEventLoop, tick, nullptr);
    if (!serverEvent || !timerEvent)
      throw std::runtime_error("guard_event_registration_failed");
    wl_event_source_timer_update(timerEvent, 50);
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
    independentSeat = std::make_unique<IndependentSeat>(h);
#endif
    return {"computer-use-guard",
            "Window-scoped seat delivery for the Computer Use broker",
            "Computer Use",
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
            "0.3.0-independent-seat"
#else
            "0.2.0"
#endif
    };
  } catch (...) {
    cleanupGuard(); // never leave callbacks into a failed/unloaded plugin
    throw;
  }
}
static void cleanupGuard() {
  clear();
#ifdef COMPUTER_USE_INDEPENDENT_SEAT
  independentSeat.reset();
#endif
  while (!peers.empty())
    closePeer(*peers.begin());
  if (timerEvent)
    wl_event_source_remove(timerEvent);
  if (serverEvent)
    wl_event_source_remove(serverEvent);
  if (serverFD >= 0)
    close(serverFD);
  timerEvent = nullptr;
  serverEvent = nullptr;
  serverFD = -1;
  if (lockFD >= 0 && !socketPath.empty())
    unlink(socketPath.c_str());
  if (lockFD >= 0) {
    flock(lockFD, LOCK_UN);
    close(lockFD);
  }
  lockFD = -1;
  socketPath.clear();
}
APICALL EXPORT void PLUGIN_EXIT() { cleanupGuard(); }
