// One bounded pointer gesture. Cleanup does not depend on a later broker call.
#pragma once
#include <cstdint>
#include <exception>
#include <string>

template <class Points, class Modifiers, class Motion, class Button,
          class Scroll>
static void pointerGesture(const std::string &kind, const Points &points,
                           uint32_t button, int clicks, uint32_t mods,
                           Modifiers modifiers, Motion motion,
                           Button sendButton, Scroll scroll) {
  bool held = false;
  try {
    modifiers(mods);
    motion(points.front());
    if (kind == "click" || kind == "drag") {
      for (int click = 0; click < (kind == "click" ? clicks : 1); ++click) {
        held = true;
        sendButton(button, true);
        if (kind == "drag")
          for (size_t i = 1; i < points.size(); ++i)
            motion(points[i]);
        sendButton(button, false);
        held = false;
      }
    } else if (kind == "scroll")
      scroll();
    modifiers(0);
  } catch (...) {
    const auto error = std::current_exception();
    // Attempt both cleanups even if one fails; fail closed on uncertainty.
    if (held)
      try {
        sendButton(button, false);
      } catch (...) {
        inputFaulted = true;
      }
    try {
      modifiers(0);
    } catch (...) {
      inputFaulted = true;
    }
    std::rethrow_exception(error);
  }
}
