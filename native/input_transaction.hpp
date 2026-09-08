// Focus borrowing is deliberately synchronous: no sleeping or event-loop
// dispatch while synthetic state is installed. This is not a second seat.
#pragma once
#include <algorithm>
#include <linux/input-event-codes.h>
#include <optional>
#include <stdexcept>
#include <string>
#include <vector>
static bool inputFaulted = false;

static void requireIdleInput() {
  if (inputFaulted)
    throw std::runtime_error("input_restore_failed_reload_plugin");
  if (g_pSeatManager->m_seatGrab || g_pInputManager->isConstrained() ||
      g_pInputManager->hasHeldButtons() ||
      (PROTO::data && PROTO::data->dndActive()) ||
      (PROTO::inputCapture && PROTO::inputCapture->isCaptured()) ||
      g_pSeatManager->m_state.touchFocus ||
      (g_pInputManager->m_relay.m_inputMethod &&
       g_pInputManager->m_relay.m_inputMethod->hasGrab()))
    throw std::runtime_error("input_busy_grab_constraint_or_drag");
  for (const auto &kb : g_pInputManager->m_keyboards) {
    if (kb->m_modifiersState.depressed || kb->m_modifiersState.latched)
      throw std::runtime_error("input_busy_keys_held");
    for (uint32_t key = 0; key <= KEY_MAX; ++key)
      if (kb->getPressed(key))
        throw std::runtime_error("input_busy_keys_held");
  }
}

static SP<IKeyboard> agentKeyboard(pid_t owner) {
  for (const auto &kb : g_pInputManager->m_keyboards) {
    if (!kb->isVirtual() || !kb->getClient() || !kb->m_enabled ||
        !kb->m_allowed || !kb->m_xkbKeymap)
      continue;
    pid_t pid = 0;
    wl_client_get_credentials(kb->getClient(), &pid, nullptr, nullptr);
    // The in-process Go device connection belongs to this broker process.
    // Do not select an unrelated virtual keyboard (e.g. an input method).
    if (pid == owner &&
        kb->m_xkbKeymapString.find("computer-use") != std::string::npos)
      return kb;
  }
  throw std::runtime_error("broker_keyboard_not_ready");
}

static SP<CWLSurfaceResource> liveSurface(WP<CWLSurfaceResource> ref) {
  auto surf = ref.lock();
  if (!surf || !surf->getResource() || !surf->getResource()->resource())
    return nullptr;
  return surf;
}

class InputTransaction {
  SP<IKeyboard> keyboard;
  SP<IPointer> mouse;
  WP<CWLSurfaceResource> kbFocus, pointerFocus, target;
  Vector2D cursor, pointerLocal;
  IKeyboard::SModifiersEvent mods;
  bool keyboardBorrowed = false, pointerBorrowed = false, restored = false;
  std::optional<uint32_t> heldKey, heldButton;
  std::vector<SP<CWLKeyboardResource>> textResources;

public:
  InputTransaction(SP<CWLSurfaceResource> surf, bool pointer)
      : keyboard(g_pSeatManager->m_keyboard.lock()),
        mouse(g_pSeatManager->m_mouse.lock()),
        kbFocus(g_pSeatManager->m_state.keyboardFocus),
        pointerFocus(g_pSeatManager->m_state.pointerFocus), target(surf),
        cursor(Pointer::mgr()->position()) {
    requireIdleInput();
    if (keyboard) {
      const auto &m = keyboard->m_modifiersState;
      mods = {m.depressed, m.latched, m.locked, m.group};
      for (const auto &kb : g_pInputManager->m_keyboards) {
        if (!kb->m_enabled || !kb->shareStates() ||
            (kb->isVirtual() &&
             g_pInputManager->shouldIgnoreVirtualKeyboard(kb)))
          continue;
        mods.depressed |= kb->m_modifiersState.depressed;
        mods.latched |= kb->m_modifiersState.latched;
        mods.locked |= kb->m_modifiersState.locked;
      }
    }
    if (pointer) {
      const auto view = Desktop::View::CWLSurface::fromResource(surf);
      if (!view || view->constraint())
        throw std::runtime_error(
            "target_pointer_constraint_or_surface_unknown");
      if (auto old = liveSurface(pointerFocus)) {
        const auto oldView = Desktop::View::CWLSurface::fromResource(old);
        const auto box =
            oldView ? oldView->getSurfaceBoxGlobal() : std::nullopt;
        if (!box || !mouse)
          throw std::runtime_error("pointer_restore_geometry_unavailable");
        pointerLocal = cursor - box->pos();
      }
    } else if (!keyboard) {
      throw std::runtime_error("keyboard_not_ready");
    }
  }

  void borrowKeyboard(SP<IKeyboard> agent) {
    keyboardBorrowed = true; // restoration also covers failed setup
    g_pSeatManager->setKeyboardFocus(nullptr);
    g_pSeatManager->setKeyboard(agent);
    g_pSeatManager->setKeyboardFocus(liveSurface(target));
    auto resource = g_pSeatManager->m_state.keyboardFocusResource.lock();
    if (!liveSurface(target) ||
        g_pSeatManager->m_state.keyboardFocus.lock() != target.lock() ||
        !resource ||
        std::ranges::none_of(resource->m_keyboards,
                             [](const auto &k) { return bool(k); }))
      throw std::runtime_error("keyboard_focus_refused");
  }

  // Send text-bearing maps ONLY to resources used for this target's input.
  // The carrier is never the seat keyboard and cannot broadcast its contents.
  void installTargetKeymap(SP<IKeyboard> map) {
    auto resource = g_pSeatManager->m_state.keyboardFocusResource.lock();
    if (!keyboardBorrowed || !keyboard || !map || !resource ||
        !textResources.empty() || !liveSurface(target) ||
        g_pSeatManager->m_state.keyboardFocus.lock() != target.lock())
      throw std::runtime_error("text_focus_refused");
    for (const auto &ref : resource->m_keyboards)
      if (auto k = ref.lock()) textResources.push_back(k);
    if (textResources.empty()) throw std::runtime_error("keyboard_focus_refused");
    for (const auto &k : textResources) k->sendKeymap(map);
  }

  void key(uint32_t code, uint32_t modifiers) {
    g_pSeatManager->sendKeyboardMods(modifiers, 0, 0, 0);
    heldKey = code;
    g_pSeatManager->sendKeyboardKey(millis(), code,
                                    WL_KEYBOARD_KEY_STATE_PRESSED);
    g_pSeatManager->sendKeyboardKey(millis(), code,
                                    WL_KEYBOARD_KEY_STATE_RELEASED);
    heldKey.reset();
    g_pSeatManager->sendKeyboardMods(0, 0, 0, 0);
  }

  void borrowPointer(const Vector2D &local) {
    SP<IPointer> agent;
    for (const auto &p : g_pInputManager->m_pointers)
      if (p->isVirtual()) {
        agent = p;
        break;
      }
    if (!agent)
      throw std::runtime_error("pointer_not_ready");
    pointerBorrowed = true;
    g_pSeatManager->setMouse(agent);
    g_pSeatManager->setPointerFocus(liveSurface(target), local);
    auto resource = g_pSeatManager->m_state.pointerFocusResource.lock();
    if (!liveSurface(target) ||
        g_pSeatManager->m_state.pointerFocus.lock() != target.lock() ||
        !resource ||
        std::ranges::none_of(resource->m_pointers,
                             [](const auto &p) { return bool(p); }))
      throw std::runtime_error("pointer_focus_refused");
  }

  void motion(const Vector2D &local) {
    g_pSeatManager->sendPointerMotion(millis(), local);
    g_pSeatManager->sendPointerFrame();
  }
  void button(uint32_t code, bool down) {
    if (down)
      heldButton = code;
    g_pSeatManager->sendPointerButton(millis(), code,
                                      down ? WL_POINTER_BUTTON_STATE_PRESSED
                                           : WL_POINTER_BUTTON_STATE_RELEASED);
    if (!down)
      heldButton.reset();
    g_pSeatManager->sendPointerFrame();
  }

  void restore() noexcept {
    if (restored)
      return;
    restored = true;
    try {
      if (keyboardBorrowed) {
        try {
          if (heldKey && liveSurface(target) &&
              g_pSeatManager->m_state.keyboardFocus.lock() == target.lock())
            g_pSeatManager->sendKeyboardKey(millis(), *heldKey,
                                            WL_KEYBOARD_KEY_STATE_RELEASED);
        } catch (...) { inputFaulted = true; }
        // Explicit restoration is required even when setKeyboard below is a
        // no-op (same device/client). Try every touched resource on failure.
        for (const auto &k : textResources) {
          try { k->sendKeymap(keyboard); }
          catch (...) { inputFaulted = true; }
        }
        g_pSeatManager->setKeyboardFocus(nullptr);
        g_pSeatManager->setKeyboard(keyboard);
        g_pSeatManager->setKeyboardFocus(liveSurface(kbFocus));
        if (liveSurface(kbFocus))
          g_pSeatManager->sendKeyboardMods(mods.depressed, mods.latched,
                                           mods.locked, mods.group);
      }
      if (pointerBorrowed) {
        if (heldButton && liveSurface(target) &&
            g_pSeatManager->m_state.pointerFocus.lock() == target.lock())
          g_pSeatManager->sendPointerButton(millis(), *heldButton,
                                            WL_POINTER_BUTTON_STATE_RELEASED);
        g_pSeatManager->sendPointerFrame();
        g_pSeatManager->setPointerFocus(liveSurface(pointerFocus),
                                        pointerLocal);
        if (liveSurface(pointerFocus))
          g_pSeatManager->sendPointerMotion(millis(), pointerLocal);
        g_pSeatManager->sendPointerFrame();
        g_pSeatManager->setMouse(mouse);
      }
      if (g_pSeatManager->m_keyboard.lock() != keyboard ||
          g_pSeatManager->m_mouse.lock() != mouse ||
          g_pSeatManager->m_state.keyboardFocus.lock() !=
              liveSurface(kbFocus) ||
          g_pSeatManager->m_state.pointerFocus.lock() !=
              liveSurface(pointerFocus) ||
          Pointer::mgr()->position() != cursor)
        inputFaulted = true;
    } catch (...) {
      inputFaulted = true;
    }
  }
  void finish() {
    restore();
    if (inputFaulted)
      throw std::runtime_error("input_restore_failed_reload_plugin");
  }
  ~InputTransaction() { restore(); }
};
