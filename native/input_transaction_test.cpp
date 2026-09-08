// Unit-test the actual transaction implementation with a fake compositor API.
// These tests cover control flow/restoration, not Hyprland wire/toolkit
// behavior.
#include <algorithm>
#include <cassert>
#include <cstdint>
#include <iostream>
#include <memory>
#include <optional>
#include <stdexcept>
#include <string>
#include <unistd.h>
#include <vector>

template <class T> using SP = std::shared_ptr<T>;
template <class T> struct WP {
  std::weak_ptr<T> value;
  WP() = default;
  WP(SP<T> v) : value(v) {}
  SP<T> lock() const { return value.lock(); }
  explicit operator bool() const { return !value.expired(); }
};
struct Vector2D {
  double x = 0, y = 0;
  Vector2D operator-(Vector2D b) const { return {x - b.x, y - b.y}; }
  bool operator==(const Vector2D &) const = default;
};
struct CBox {
  Vector2D origin;
  Vector2D pos() const { return origin; }
};
struct Resource {
  void *resource() { return this; }
};
struct CWLSurfaceResource {
  SP<Resource> resource = std::make_shared<Resource>();
  bool constrained = false, hasBox = true;
  CBox box{{10, 20}};
  SP<Resource> getResource() { return resource; }
};
namespace Desktop::View {
struct CWLSurface {
  SP<CWLSurfaceResource> surface;
  static SP<CWLSurface> fromResource(SP<CWLSurfaceResource> s) {
    return s ? std::make_shared<CWLSurface>(CWLSurface{s}) : nullptr;
  }
  bool constraint() { return surface->constrained; }
  std::optional<CBox> getSurfaceBoxGlobal() {
    return surface->hasBox ? std::optional(surface->box) : std::nullopt;
  }
};
} // namespace Desktop::View
struct wl_client {
  pid_t pid;
};
static void wl_client_get_credentials(wl_client *c, pid_t *pid, void *,
                                      void *) {
  *pid = c->pid;
}
struct IKeyboard {
  struct SModifiersEvent {
    uint32_t depressed = 0, latched = 0, locked = 0, group = 0;
  };
  SModifiersEvent m_modifiersState;
  bool virt = false, m_enabled = true, m_allowed = true, held = false;
  void *m_xkbKeymap = this;
  std::string m_xkbKeymapString = "computer-use";
  wl_client client{getpid()};
  bool isVirtual() { return virt; }
  bool shareStates() { return true; }
  bool getPressed(uint32_t code) { return held && code == 30; }
  wl_client *getClient() { return &client; }
};
struct IPointer {
  bool virt = false;
  bool isVirtual() { return virt; }
};
struct InputMethod {
  bool grab = false;
  bool hasGrab() { return grab; }
};
struct InputManager {
  bool constrained = false, buttons = false;
  struct {
    SP<InputMethod> m_inputMethod;
  } m_relay;
  std::vector<SP<IKeyboard>> m_keyboards;
  std::vector<SP<IPointer>> m_pointers;
  bool isConstrained() { return constrained; }
  bool hasHeldButtons() { return buttons; }
  bool shouldIgnoreVirtualKeyboard(SP<IKeyboard>) { return false; }
};
constexpr int WL_KEYBOARD_KEY_STATE_RELEASED = 0,
              WL_KEYBOARD_KEY_STATE_PRESSED = 1;
constexpr int WL_POINTER_BUTTON_STATE_RELEASED = 0,
              WL_POINTER_BUTTON_STATE_PRESSED = 1;
static uint32_t millis() { return 1; }
struct CWLKeyboardResource {
  std::vector<SP<IKeyboard>> maps;
  int failAt = 0;
  void sendKeymap(SP<IKeyboard> map) {
    maps.push_back(map);
    if (int(maps.size()) == failAt) throw std::runtime_error("keymap_test_failure");
  }
};
struct Seat {
  SP<CWLKeyboardResource> targetKeys = std::make_shared<CWLKeyboardResource>();
  SP<CWLKeyboardResource> unrelatedKeys = std::make_shared<CWLKeyboardResource>();
  struct FocusResource {
    std::vector<WP<CWLKeyboardResource>> m_keyboards;
    std::vector<bool> m_pointers{true};
  };
  SP<FocusResource> focusResource = std::make_shared<FocusResource>();
  WP<IKeyboard> m_keyboard;
  WP<IPointer> m_mouse;
  bool m_seatGrab = false;
  struct {
    WP<CWLSurfaceResource> keyboardFocus, pointerFocus, touchFocus;
    WP<FocusResource> keyboardFocusResource, pointerFocusResource;
  } m_state;
  Seat() {
    focusResource->m_keyboards = {targetKeys};
    m_state.keyboardFocusResource = focusResource;
    m_state.pointerFocusResource = focusResource;
  }
  std::vector<std::string> events;
  std::string failOn;
  int failKeyDownAt = 0, keyDowns = 0;
  IKeyboard::SModifiersEvent sentMods;
  Vector2D local;
  void event(const std::string &s) {
    events.push_back(s);
    if (failOn == s) {
      failOn = "";
      throw std::runtime_error("test failure");
    }
  }
  void setKeyboardFocus(SP<CWLSurfaceResource> s) {
    event("keyboard_focus");
    m_state.keyboardFocus = s;
  }
  void setKeyboard(SP<IKeyboard> k) {
    // Fails even on transient global installation of a private text map.
    assert(!k || k->m_xkbKeymapString.find("text-chunk") == std::string::npos);
    event("keyboard");
    m_keyboard = k;
  }
  void sendKeyboardMods(uint32_t d, uint32_t l, uint32_t k, uint32_t g) {
    event("mods");
    sentMods = {d, l, k, g};
  }
  void sendKeyboardKey(uint32_t, uint32_t, int state) {
    if (state && ++keyDowns == failKeyDownAt) throw std::runtime_error("partial_text_test");
    event(state ? "key_down" : "key_up");
  }
  void setMouse(SP<IPointer> p) {
    event("mouse");
    m_mouse = p;
  }
  void setPointerFocus(SP<CWLSurfaceResource> s, Vector2D p) {
    event("pointer_focus");
    m_state.pointerFocus = s;
    local = p;
  }
  void sendPointerMotion(uint32_t, Vector2D p) {
    event("motion");
    local = p;
  }
  void sendPointerFrame() { event("frame"); }
  void sendPointerButton(uint32_t, uint32_t, int state) {
    event(state ? "button_down" : "button_up");
  }
};
struct Data {
  bool active = false;
  bool dndActive() { return active; }
  bool isCaptured() { return active; }
};
static auto g_pSeatManager = std::make_unique<Seat>();
static auto g_pInputManager = std::make_unique<InputManager>();
namespace PROTO {
static auto data = std::make_unique<Data>();
static auto inputCapture = std::make_unique<Data>();
} // namespace PROTO
namespace Pointer {
struct Manager {
  Vector2D cursor{100, 200};
  Vector2D position() { return cursor; }
};
static Manager manager;
static Manager *mgr() { return &manager; }
} // namespace Pointer
#include "input_transaction.hpp"
#include "text_transaction.hpp"

struct Fixture {
  SP<IKeyboard> human = std::make_shared<IKeyboard>(),
                agent = std::make_shared<IKeyboard>();
  SP<IPointer> mouse = std::make_shared<IPointer>(),
               virtualMouse = std::make_shared<IPointer>();
  SP<CWLSurfaceResource> original = std::make_shared<CWLSurfaceResource>(),
                         target = std::make_shared<CWLSurfaceResource>();
  Fixture() {
    inputFaulted = false;
    g_pSeatManager = std::make_unique<Seat>();
    g_pInputManager = std::make_unique<InputManager>();
    PROTO::data = std::make_unique<Data>();
    PROTO::inputCapture = std::make_unique<Data>();
    human->m_modifiersState.locked = 2;
    human->m_modifiersState.group = 1;
    agent->virt = true;
    virtualMouse->virt = true;
    g_pInputManager->m_keyboards = {human, agent};
    g_pInputManager->m_pointers = {mouse, virtualMouse};
    g_pSeatManager->m_keyboard = human;
    g_pSeatManager->m_mouse = mouse;
    g_pSeatManager->m_state.keyboardFocus = original;
    g_pSeatManager->m_state.pointerFocus = original;
  }
  void restored() {
    assert(!inputFaulted);
    assert(g_pSeatManager->m_keyboard.lock() == human &&
           g_pSeatManager->m_mouse.lock() == mouse);
    assert(g_pSeatManager->m_state.keyboardFocus.lock() == original &&
           g_pSeatManager->m_state.pointerFocus.lock() == original);
    assert((Pointer::mgr()->position() == Vector2D{100, 200}));
  }
};
static SP<IKeyboard> makeTextMap(const std::string &map) {
  auto keyboard = std::make_shared<IKeyboard>();
  keyboard->m_xkbKeymapString = map;
  return keyboard;
}
int main(int argc, char **argv) {
  if (argc > 1 && std::string(argv[1]) == "--text-keymap") {
    std::vector<uint32_t> scalars;
    for (int i = 2; i < argc; ++i) scalars.push_back(std::stoul(argv[i]));
    std::cout << textKeymap(scalars);
    return 0;
  }
  {
    Fixture f;
    std::vector<uint32_t> scalars{0x41,0xe9,0x4e16,0x1f680,0x301,9,10};
    textTransaction(f.target,f.agent,scalars,makeTextMap);
    assert(g_pSeatManager->keyDowns==7);
    auto &maps = g_pSeatManager->targetKeys->maps;
    assert(maps.size() == 2 && maps[0]->m_xkbKeymapString == textKeymap(scalars) && maps[1] == f.human);
    assert(g_pSeatManager->unrelatedKeys->maps.empty());
    f.restored();
  }
  for (auto bad : {0u,13u,0x7fu,0x85u,0xd800u,0x110000u}) {
    Fixture f;
    try { textTransaction(f.target,f.agent,{65,bad},makeTextMap); assert(false); }
    catch(const TextFailure &e) { assert(e.completed==0); }
    assert(g_pSeatManager->events.empty());
  }
  for (size_t size : {size_t(0), TEXT_CHUNK_RUNES + 1}) {
    Fixture f;
    try { textTransaction(f.target,f.agent,std::vector<uint32_t>(size,65),makeTextMap); assert(false); }
    catch(const TextFailure &e) { assert(e.completed==0); }
    assert(g_pSeatManager->events.empty());
  }
  {
    Fixture f;
    try { textTransaction(f.target,f.agent,{65},[](const std::string &) -> SP<IKeyboard> { throw std::runtime_error("allocation_failed"); }); assert(false); }
    catch(const TextFailure &e) { assert(e.completed==0); }
    assert(g_pSeatManager->events.empty());
  }
  {
    Fixture f;
    g_pSeatManager->failKeyDownAt=3;
    try { textTransaction(f.target,f.agent,{65,66,67,68},makeTextMap); assert(false); }
    catch(const TextFailure &e) { assert(e.completed==2); }
    f.restored();
  }

  {
    Fixture f;
    // No device switch at all; temporary target map still must be restored.
    g_pSeatManager->m_keyboard = f.agent;
    textTransaction(f.target,f.agent,{65},makeTextMap);
    assert(g_pSeatManager->targetKeys->maps.back() == f.agent);
    assert(g_pSeatManager->unrelatedKeys->maps.empty());
    assert(g_pSeatManager->m_keyboard.lock() == f.agent);
  }
  {
    Fixture f;
    g_pSeatManager->targetKeys->failAt = 1;
    try { textTransaction(f.target,f.agent,{65},makeTextMap); assert(false); }
    catch (const TextFailure &e) { assert(e.completed == 0); }
    assert(g_pSeatManager->targetKeys->maps.back() == f.human);
    f.restored();
  }
  {
    Fixture f;
    g_pSeatManager->targetKeys->failAt = 2;
    try { textTransaction(f.target,f.agent,{65},makeTextMap); assert(false); }
    catch (const TextFailure &e) { assert(e.completed == 1); }
    assert(inputFaulted);
    assert(g_pSeatManager->m_keyboard.lock() == f.human);
  }
  for (bool failRestore : {false, true}) {
    Fixture f;
    auto second = std::make_shared<CWLKeyboardResource>();
    g_pSeatManager->focusResource->m_keyboards.push_back(second);
    if (failRestore) g_pSeatManager->targetKeys->failAt = 2;
    try {
      textTransaction(f.target, f.agent, {65, 66}, makeTextMap);
      assert(!failRestore);
    } catch (const TextFailure &e) { assert(failRestore && e.completed == 2); }
    assert(second->maps.size() == 2 && second->maps.back() == f.human);
    assert(inputFaulted == failRestore);
  }
  {
    Fixture f;
    InputTransaction t(f.target, false);
    try { t.installTargetKeymap(makeTextMap(textKeymap({65}))); assert(false); }
    catch (const std::runtime_error &) {}
    assert(g_pSeatManager->targetKeys->maps.empty());
  }
  {
    Fixture f;
    g_pSeatManager->m_keyboard = SP<IKeyboard>{};
    try { textTransaction(f.target, f.agent, {65}, makeTextMap); assert(false); }
    catch (const TextFailure &e) { assert(e.completed == 0); }
    assert(g_pSeatManager->events.empty() && g_pSeatManager->targetKeys->maps.empty());
  }
  {
    Fixture f;
    InputTransaction t(f.target, false);
    t.borrowKeyboard(agentKeyboard(getpid()));
    t.key(30, 1);
    t.finish();
    f.restored();
    assert(g_pSeatManager->sentMods.locked == 2 &&
           g_pSeatManager->sentMods.group == 1);
    assert(std::count(g_pSeatManager->events.begin(),
                      g_pSeatManager->events.end(), "key_down") == 1);
    assert(std::count(g_pSeatManager->events.begin(),
                      g_pSeatManager->events.end(), "key_up") == 1);
  }
  {
    Fixture f;
    try {
      InputTransaction t(f.target, false);
      t.borrowKeyboard(f.agent);
      g_pSeatManager->failOn = "key_down";
      t.key(30, 1);
      assert(false);
    } catch (const std::runtime_error &) {
    }
    f.restored();
    assert(std::count(g_pSeatManager->events.begin(),
                      g_pSeatManager->events.end(), "key_up") == 1);
  }
  {
    Fixture f;
    InputTransaction t(f.target, true);
    t.borrowPointer({30, 40});
    t.motion({30, 40});
    t.button(272, true);
    t.motion({50, 60});
    t.button(272, false);
    t.finish();
    f.restored();
    assert((g_pSeatManager->local == Vector2D{90, 180}));
  }
  {
    Fixture f;
    try {
      InputTransaction t(f.target, true);
      t.borrowPointer({30, 40});
      t.button(272, true);
      g_pSeatManager->failOn = "motion";
      t.motion({50, 60});
      assert(false);
    } catch (const std::runtime_error &) {
    }
    f.restored();
    assert(std::count(g_pSeatManager->events.begin(),
                      g_pSeatManager->events.end(), "button_up") == 1);
  }
  for (int busy = 0; busy < 8; ++busy) {
    Fixture f;
    switch (busy) {
    case 0:
      f.human->held = true;
      break;
    case 1:
      g_pInputManager->buttons = true;
      break;
    case 2:
      g_pSeatManager->m_seatGrab = true;
      break;
    case 3:
      g_pInputManager->constrained = true;
      break;
    case 4:
      PROTO::data->active = true;
      break;
    case 5:
      f.human->m_modifiersState.depressed = 1;
      break;
    case 6:
      g_pInputManager->m_relay.m_inputMethod = std::make_shared<InputMethod>();
      g_pInputManager->m_relay.m_inputMethod->grab = true;
      break;
    case 7:
      PROTO::inputCapture->active = true;
      break;
    }
    bool refused = false;
    try {
      InputTransaction t(f.target, true);
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused && g_pSeatManager->events.empty());
  }
  {
    Fixture f;
    f.agent->client.pid = getpid() + 1;
    bool refused = false;
    try {
      agentKeyboard(getpid());
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused);
  }
  {
    Fixture f;
    f.original->hasBox = false;
    bool refused = false;
    try {
      InputTransaction t(f.target, true);
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused && g_pSeatManager->events.empty());
  }
  {
    Fixture f;
    InputTransaction t(f.target, true);
    t.borrowPointer({1, 2});
    g_pSeatManager->failOn = "pointer_focus";
    bool refused = false;
    try {
      t.finish();
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused && inputFaulted);
  }
  {
    Fixture f;
    g_pSeatManager->focusResource->m_keyboards.clear();
    bool refused = false;
    try {
      InputTransaction t(f.target, false);
      t.borrowKeyboard(f.agent);
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused);
    f.restored();
  }
  {
    Fixture f;
    g_pSeatManager->focusResource->m_pointers.clear();
    bool refused = false;
    try {
      InputTransaction t(f.target, true);
      t.borrowPointer({1, 2});
    } catch (const std::runtime_error &) {
      refused = true;
    }
    assert(refused);
    f.restored();
  }
  std::cout
      << "focus-preserving transaction restoration/refusal tests passed\n";
}
