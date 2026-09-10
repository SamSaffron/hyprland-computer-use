// Independent protocol seat for native Wayland windows only.
// No native focus/device setters, global injection, clipboard or IME delivery.
// Exact-version popup interception is a compatibility bridge, not a stable API.
#pragma once
#include "popup_hook.hpp"
#include "seat_policy.hpp"
#include <cstring>
#include <deque>
#include <memory>
#include <wayland-server-protocol.h>
#include <xkbcommon/xkbcommon.h>

class IndependentSeat {
  inline static IndependentSeat *self = nullptr;
  wl_global *global = nullptr;
  wl_protocol_logger *logger = nullptr;
  CFunctionHook *grabHook = nullptr;
  std::vector<wl_resource *> resources, keyboards, pointers;
  wl_listener rootDestroy{}, focusDestroy{};
  wl_resource *rootRaw = nullptr, *focusRaw = nullptr;
  WP<CWLSurfaceResource> root, focus;
  std::string token;
  SeatSerials serials;
  SP<TextKeyboard> baseMap;
  WP<CXDGPopupResource> pending;
  bool pendingAllowed = false;
  struct Popup {
    WP<CXDGPopupResource> resource;
    bool mapped = false;
  };
  std::vector<Popup> popups;
  size_t refusedGrabs = 0;

  static uint32_t nextSerial() {
    return wl_display_next_serial(g_pCompositor->m_wlDisplay);
  }
  static wl_resource *raw(SP<CWLSurfaceResource> s) {
    return s && s->good() ? s->getResource()->resource() : nullptr;
  }
  static void unlink(wl_listener &l) {
    wl_list_remove(&l.link);
    wl_list_init(&l.link);
  }
  static void rootGone(wl_listener *, void *) { self->reset(false); }
  static void focusGone(wl_listener *, void *) {
    unlink(self->focusDestroy);
    self->focusRaw = nullptr;
    self->focus.reset();
  }
  static void destroyed(wl_resource *r) {
    std::erase(self->resources, r);
    std::erase(self->keyboards, r);
    std::erase(self->pointers, r);
  }
  static void release(wl_client *, wl_resource *r) { wl_resource_destroy(r); }
  static void cursor(wl_client *, wl_resource *, uint32_t, wl_resource *,
                     int32_t, int32_t) {}
  inline static const struct wl_pointer_interface pointerImpl = {cursor,
                                                                 release};
  inline static const struct wl_keyboard_interface keyboardImpl = {release};
  inline static const struct wl_touch_interface touchImpl = {release};
  static wl_resource *create(wl_client *c, wl_resource *seat, uint32_t id,
                             const wl_interface *iface, const void *impl) {
    auto r = wl_resource_create(c, iface, wl_resource_get_version(seat), id);
    if (!r) {
      wl_client_post_no_memory(c);
      return nullptr;
    }
    wl_resource_set_implementation(r, impl, nullptr, destroyed);
    self->resources.push_back(r);
    return r;
  }
  static void getKeyboard(wl_client *c, wl_resource *s, uint32_t id) {
    auto r = create(c, s, id, &wl_keyboard_interface, &keyboardImpl);
    if (!r)
      return;
    self->keyboards.push_back(r);
    self->map(r, self->baseMap);
    if (wl_resource_get_version(r) >= 4)
      wl_keyboard_send_repeat_info(r, 0, 0);
    // Binding a resource cannot create authority. Revalidate the existing
    // lease.
    if (self->authorized() && self->focusRaw && self->focus &&
        self->focus->client() == c)
      self->enterKeyboard(r);
  }
  static void getPointer(wl_client *c, wl_resource *s, uint32_t id) {
    auto r = create(c, s, id, &wl_pointer_interface, &pointerImpl);
    if (r)
      self->pointers.push_back(r);
  }
  static void getTouch(wl_client *c, wl_resource *s, uint32_t id) {
    create(c, s, id, &wl_touch_interface,
           &touchImpl); // touch capability is never advertised
  }
  inline static const struct wl_seat_interface seatImpl = {
      getPointer, getKeyboard, getTouch, release};
  static void bindSeat(wl_client *c, void *, uint32_t version, uint32_t id) {
    auto r =
        wl_resource_create(c, &wl_seat_interface, std::min(version, 7u), id);
    if (!r) {
      wl_client_post_no_memory(c);
      return;
    }
    wl_resource_set_implementation(r, &seatImpl, nullptr, destroyed);
    self->resources.push_back(r);
    wl_seat_send_capabilities(r, WL_SEAT_CAPABILITY_KEYBOARD |
                                     WL_SEAT_CAPABILITY_POINTER);
    if (version >= 2)
      wl_seat_send_name(r, "computer-use-agent");
  }
  void map(wl_resource *k, SP<TextKeyboard> m) {
    wl_keyboard_send_keymap(k, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1,
                            m->m_xkbKeymapV1FD.get(),
                            m->m_xkbKeymapV1String.size() + 1);
  }
  void enterKeyboard(wl_resource *k) {
    wl_array keys;
    wl_array_init(&keys);
    wl_keyboard_send_enter(k, nextSerial(), focusRaw, &keys);
    wl_array_release(&keys);
    wl_keyboard_send_modifiers(k, nextSerial(), 0, 0, 0, 0);
  }
  bool authorized() const {
    auto it = leases.find(token);
    return !locked() && rootRaw && root && it != leases.end() &&
           Clock::now() < it->second.until && it->second.window &&
           it->second.window->m_isMapped && it->second.window->visible() &&
           it->second.window->acceptsInput() &&
           it->second.surface.lock() == root.lock() &&
           it->second.window->resource() == root.lock() &&
           raw(root.lock()) == rootRaw;
  }
  bool belongs(SP<CWLSurfaceResource> surface) const {
    if (!authorized() || !surface || !surface->m_mapped || !raw(surface))
      return false;
    auto w = leases.at(token).window.lock();
    return treeContains(windowSurfaceTree(w), surface);
  }
  void select(SP<CWLSurfaceResource> surface, Vector2D local) {
    if (!belongs(surface))
      throw std::runtime_error("agent_surface_outside_live_lease");
    if (focusRaw == raw(surface))
      return;
    leave();
    focus = surface;
    focusRaw = raw(surface);
    wl_resource_add_destroy_listener(focusRaw, &focusDestroy);
    for (auto k : keyboards)
      if (wl_resource_get_client(k) == surface->client())
        enterKeyboard(k);
    for (auto p : pointers)
      if (wl_resource_get_client(p) == surface->client()) {
        wl_pointer_send_enter(p, nextSerial(), focusRaw,
                              wl_fixed_from_double(local.x),
                              wl_fixed_from_double(local.y));
        frame(p);
      }
  }
  void leave() {
    auto old = focus.lock();
    if (focusRaw && old) {
      for (auto k : keyboards)
        if (wl_resource_get_client(k) == old->client()) {
          wl_keyboard_send_modifiers(k, nextSerial(), 0, 0, 0, 0);
          wl_keyboard_send_leave(k, nextSerial(), focusRaw);
        }
      for (auto p : pointers)
        if (wl_resource_get_client(p) == old->client()) {
          wl_pointer_send_leave(p, nextSerial(), focusRaw);
          frame(p);
        }
    }
    unlink(focusDestroy);
    focus.reset();
    focusRaw = nullptr;
  }
  static void frame(wl_resource *p) {
    if (wl_resource_get_version(p) >= 5)
      wl_pointer_send_frame(p);
  }
  uint32_t inputSerial() {
    auto s = nextSerial();
    serials.issue(s, token, reinterpret_cast<uintptr_t>(rootRaw),
                  std::min(leases.at(token).until,
                           Clock::now() + std::chrono::seconds(5)));
    return s;
  }
  // Logger attribution is synchronous with the immediately following request
  // callback. Only an agent-seat request for this exact popup is intercepted.
  static void protocol(void *, wl_protocol_logger_type type,
                       const wl_protocol_logger_message *m) {
    if (type != WL_PROTOCOL_LOGGER_REQUEST)
      return;
    self->pending.reset();
    self->pendingAllowed = false;
    if (std::strcmp(wl_resource_get_class(m->resource), "xdg_popup") ||
        std::strcmp(m->message->name, "grab") || m->arguments_count != 2)
      return;
    auto seat = reinterpret_cast<wl_resource *>(m->arguments[0].o);
    if (!seat || !wl_resource_instance_of(seat, &wl_seat_interface, &seatImpl))
      return;
    auto popup = CXDGPopupResource::fromResource(m->resource);
    self->pending = popup;
    if (!popup || !self->authorized() || self->popups.size() >= 16)
      return;
    auto parent = popup->m_parent.lock();
    // The new popup need not be mapped yet; its parent MUST be in the live
    // tree.
    try {
      if (!parent || !self->belongs(parent->m_surface.lock()) ||
          wl_resource_get_client(seat) != self->root->client())
        return;
      self->pendingAllowed = self->serials.consume(
          m->arguments[1].u, self->token,
          reinterpret_cast<uintptr_t>(self->rootRaw), Clock::now());
    } catch (...) {
      self->pendingAllowed = false;
    }
  }
  static void grab(CXDGShellProtocol *protocol, SP<CXDGPopupResource> popup) {
    if (self->pending.lock() == popup) {
      self->pending.reset();
      if (self->pendingAllowed && self->authorized())
        self->popups.push_back({popup, false});
      else {
        ++self->refusedGrabs;
        popup->done();
      }
      self->pendingAllowed = false;
      return;
    }
    reinterpret_cast<void (*)(CXDGShellProtocol *, SP<CXDGPopupResource>)>(
        self->grabHook->m_original)(protocol, popup);
  }

public:
  explicit IndependentSeat(HANDLE h) {
    self = this;
    wl_list_init(&rootDestroy.link);
    wl_list_init(&focusDestroy.link);
    rootDestroy.notify = rootGone;
    focusDestroy.notify = focusGone;
    xkb_context *ctx = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
    if (!ctx)
      throw std::runtime_error("agent_keymap_context_failed");
    const xkb_rule_names names = {nullptr, "pc105", "us", "", ""};
    auto km =
        xkb_keymap_new_from_names(ctx, &names, XKB_KEYMAP_COMPILE_NO_FLAGS);
    if (!km) {
      xkb_context_unref(ctx);
      throw std::runtime_error("agent_keymap_failed");
    }
    char *text = xkb_keymap_get_as_string(km, XKB_KEYMAP_FORMAT_TEXT_V1);
    xkb_keymap_unref(km);
    xkb_context_unref(ctx);
    if (!text)
      throw std::runtime_error("agent_keymap_failed");
    std::unique_ptr<char, decltype(&free)> mapText(text, free);
    baseMap = makeShared<TextKeyboard>(std::string(text));
    auto address = independentPopupGrabAddress();
    if (!address)
      throw std::runtime_error("agent_popup_symbol_unavailable");
    grabHook = HyprlandAPI::createFunctionHook(h, address,
                                               reinterpret_cast<void *>(grab));
    if (!grabHook || !grabHook->hook())
      throw std::runtime_error("agent_popup_hook_unavailable");
    logger = wl_display_add_protocol_logger(g_pCompositor->m_wlDisplay,
                                            protocol, nullptr);
    if (!logger) {
      grabHook->unhook();
      throw std::runtime_error("agent_logger_failed");
    }
    global = wl_global_create(g_pCompositor->m_wlDisplay, &wl_seat_interface, 7,
                              nullptr, bindSeat);
    if (!global) {
      wl_protocol_logger_destroy(logger);
      grabHook->unhook();
      throw std::runtime_error("agent_seat_failed");
    }
  }
  ~IndependentSeat() {
    reset();
    if (logger)
      wl_protocol_logger_destroy(logger);
    if (grabHook)
      grabHook->unhook();
    while (!resources.empty())
      wl_resource_destroy(resources.back());
    if (global)
      wl_global_destroy(global);
    self = nullptr;
  }
  void reset(bool notify = true) {
    if (notify) {
      for (auto it = popups.rbegin(); it != popups.rend(); ++it)
        if (auto p = it->resource.lock())
          p->done();
      leave();
    } else {
      unlink(focusDestroy);
      focus.reset();
      focusRaw = nullptr;
    }
    unlink(rootDestroy);
    root.reset();
    rootRaw = nullptr;
    token.clear();
    serials.clear();
    popups.clear();
    pending.reset();
    pendingAllowed = false;
  }
  void revoke(const std::string &lease) {
    if (lease == token)
      reset();
  }
  void tick() {
    if (!token.empty() && !authorized())
      reset();
  }
  // Require both devices so a window never mixes seat and fallback delivery.
  bool supports(SP<CWLSurfaceResource> surface) const {
    if (!surface)
      return false;
    const auto bound = [&](const auto &list) {
      return std::ranges::any_of(list, [&](auto r) {
        return wl_resource_get_client(r) == surface->client();
      });
    };
    return bound(keyboards) && bound(pointers);
  }
  size_t clients() const { return resources.size(); }
  size_t deniedGrabs() const { return refusedGrabs; }
  void begin(const std::string &lease, SP<CWLSurfaceResource> rootSurface,
             SP<CWLSurfaceResource> surface, Vector2D local = {},
             bool keyboard = false, bool explicitSurface = false) {
    // Constraints, capture, DnD and IME grabs are NOT independently integrated.
    // Do not refuse ordinary physical typing or a native human popup grab.
    if (g_pInputManager->isConstrained() ||
        (PROTO::data && PROTO::data->dndActive()) ||
        (PROTO::inputCapture && PROTO::inputCapture->isCaptured()) ||
        g_pSeatManager->m_state.touchFocus ||
        (g_pInputManager->m_relay.m_inputMethod &&
         g_pInputManager->m_relay.m_inputMethod->hasGrab()))
      throw std::runtime_error(
          "agent_seat_constraint_capture_drag_or_ime_unsupported");
    if (token != lease || rootRaw != raw(rootSurface)) {
      reset();
      token = lease;
      root = rootSurface;
      rootRaw = raw(rootSurface);
      if (!rootRaw) {
        reset();
        throw std::runtime_error("target_closed");
      }
      wl_resource_add_destroy_listener(rootRaw, &rootDestroy);
    }
    tick();
    if (!authorized())
      throw std::runtime_error("lease_expired_or_revoked");
    std::erase_if(popups, [](auto &entry) {
      auto p = entry.resource.lock();
      if (!p || !p->good() || !p->m_surface)
        return true;
      if (p->m_surface->m_mapped) {
        entry.mapped = true;
        return false;
      }
      return entry
          .mapped; // never redirect a not-yet-mapped popup to its parent
    });
    if (keyboard && !explicitSurface && !popups.empty()) {
      auto p = popups.back().resource.lock();
      if (!p || !p->m_surface || !p->m_surface->m_mapped)
        throw std::runtime_error("agent_popup_not_mapped");
      surface = p->m_surface->m_surface.lock();
    }
    const auto view = Desktop::View::CWLSurface::fromResource(surface);
    if (!view || view->constraint())
      throw std::runtime_error("agent_target_constraint_unsupported");
    auto &list = keyboard ? keyboards : pointers;
    if (std::ranges::none_of(list, [&](auto r) {
          return wl_resource_get_client(r) == surface->client();
        }))
      throw std::runtime_error(
          "application_did_not_bind_agent_seat_restart_application");
    select(surface, local);
  }
  void key(uint32_t code, uint32_t modifiers) {
    if (!authorized() || !focusRaw || !focus || !focus->m_mapped)
      throw std::runtime_error("agent_focus_unavailable");
    for (auto k : keyboards)
      if (wl_resource_get_client(k) == focus->client()) {
        // Allocate the authority record before any modifiers/events.
        const auto pressSerial = inputSerial();
        wl_keyboard_send_modifiers(k, nextSerial(), modifiers, 0, 0, 0);
        wl_keyboard_send_key(k, pressSerial, millis(), code,
                             WL_KEYBOARD_KEY_STATE_PRESSED);
        wl_keyboard_send_key(k, nextSerial(), millis(), code,
                             WL_KEYBOARD_KEY_STATE_RELEASED);
        wl_keyboard_send_modifiers(k, nextSerial(), 0, 0, 0, 0);
      }
  }
  void text(const std::vector<uint32_t> &scalars) {
    auto textMap = makeShared<TextKeyboard>(textKeymap(scalars));
    size_t completed = 0;
    try {
      for (auto k : keyboards)
        if (wl_resource_get_client(k) == focus->client())
          map(k, textMap);
      for (size_t i = 0; i < scalars.size(); ++i) {
        key(TEXT_KEY_CODES[i], 0);
        ++completed;
      }
    } catch (const std::exception &e) {
      for (auto k : keyboards)
        if (focus && wl_resource_get_client(k) == focus->client())
          map(k, baseMap);
      throw TextFailure(e.what(), completed);
    }
    for (auto k : keyboards)
      if (wl_resource_get_client(k) == focus->client())
        map(k, baseMap);
  }
  void pointerModifiers(uint32_t mods) {
    if (!focus || !focusRaw)
      throw std::runtime_error("agent_focus_unavailable");
    for (auto k : keyboards)
      if (wl_resource_get_client(k) == focus->client())
        wl_keyboard_send_modifiers(k, nextSerial(), mods, 0, 0, 0);
  }
  void scrollAxes(double x, double y, bool steps) {
    for (auto p : pointers) {
      if (wl_resource_get_client(p) != focus->client())
        continue;
      const auto version = wl_resource_get_version(p);
      if (version >= 5)
        wl_pointer_send_axis_source(p, steps
                                           ? WL_POINTER_AXIS_SOURCE_WHEEL
                                           : WL_POINTER_AXIS_SOURCE_CONTINUOUS);
      for (int axis = 0; axis < 2; ++axis) {
        const double value = axis == 0 ? y : x;
        if (value == 0)
          continue;
        if (steps) {
          if (version >= 8)
            wl_pointer_send_axis_value120(p, axis, int32_t(value * 120));
          else if (version >= 5)
            wl_pointer_send_axis_discrete(p, axis, int32_t(value));
        }
        wl_pointer_send_axis(p, millis(), axis,
                             wl_fixed_from_double(value * (steps ? 10 : 1)));
      }
      frame(p);
    }
  }
  void motion(Vector2D local) {
    for (auto p : pointers)
      if (wl_resource_get_client(p) == focus->client()) {
        wl_pointer_send_motion(p, millis(), wl_fixed_from_double(local.x),
                               wl_fixed_from_double(local.y));
        frame(p);
      }
  }
  void button(uint32_t code, bool down) {
    for (auto p : pointers)
      if (wl_resource_get_client(p) == focus->client()) {
        wl_pointer_send_button(p, down ? inputSerial() : nextSerial(), millis(),
                               code,
                               down ? WL_POINTER_BUTTON_STATE_PRESSED
                                    : WL_POINTER_BUTTON_STATE_RELEASED);
        frame(p);
      }
  }
  void scroll(double delta) {
    for (auto p : pointers)
      if (wl_resource_get_client(p) == focus->client()) {
        if (wl_resource_get_version(p) >= 5)
          wl_pointer_send_axis_source(p, WL_POINTER_AXIS_SOURCE_WHEEL);
        wl_pointer_send_axis(p, millis(), WL_POINTER_AXIS_VERTICAL_SCROLL,
                             wl_fixed_from_double(delta));
        frame(p);
      }
  }
};
static std::unique_ptr<IndependentSeat> independentSeat;
