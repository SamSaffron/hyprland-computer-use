#include <cassert>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>
static bool inputFaulted = false;
#include "pointer_gesture.hpp"
int main() {
  for (const auto &kind : {std::string("click"), std::string("drag"),
                           std::string("scroll"), std::string("move")}) {
    std::vector<std::string> events;
    pointerGesture(
        kind, std::vector<int>{1, 2, 3}, 272, 3, 4,
        [&](uint32_t m) { events.push_back("mods" + std::to_string(m)); },
        [&](int p) { events.push_back("move" + std::to_string(p)); },
        [&](uint32_t, bool down) { events.push_back(down ? "down" : "up"); },
        [&] { events.push_back("scroll"); });
    assert(events.front() == "mods4" && events.back() == "mods0");
    if (kind == "click")
      assert((events == std::vector<std::string>{"mods4", "move1", "down", "up",
                                                 "down", "up", "down", "up",
                                                 "mods0"}));
    if (kind == "drag")
      assert(
          (events == std::vector<std::string>{"mods4", "move1", "down", "move2",
                                              "move3", "up", "mods0"}));
  }
  // Lost delivery/exception at each stage still attempts release and modifier
  // cleanup in the SAME call. Never wait for another broker action to release.
  for (int failAt = 0; failAt < 7; ++failAt) {
    bool held = false;
    uint32_t mods = 0;
    int step = 0;
    auto fail = [&] {
      if (step++ == failAt)
        throw std::runtime_error("delivery");
    };
    try {
      pointerGesture(
          "drag", std::vector<int>{1, 2, 3}, 272, 1, 4,
          [&](uint32_t m) {
            mods = m;
            fail();
          },
          [&](int) { fail(); },
          [&](uint32_t, bool down) {
            held = down;
            fail();
          },
          [] {});
      assert(false);
    } catch (const std::runtime_error &e) {
      assert(std::string(e.what()) == "delivery");
    }
    assert(!held && mods == 0 && !inputFaulted);
  }
  bool reset = false;
  try {
    pointerGesture(
        "drag", std::vector<int>{1, 2}, 272, 1, 4,
        [&](uint32_t m) {
          if (!m)
            reset = true;
        },
        [](int) {},
        [](uint32_t, bool) { throw std::runtime_error("button failure"); },
        [] {});
    assert(false);
  } catch (const std::runtime_error &) {
  }
  assert(inputFaulted && reset);
  std::cout << "pointer gesture ordering, multi-click, path and exception "
               "cleanup tests passed\n";
}
