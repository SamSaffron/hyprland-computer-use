// Bounded Unicode key transactions. No event-loop dispatch, sleeps, clipboard
// or global injection. The caller has already validated the live surface lease.
#pragma once
#include "text_keymap.hpp"

struct TextFailure : std::runtime_error {
  size_t completed;
  TextFailure(const std::string &message, size_t count)
      : std::runtime_error(message), completed(count) {}
};

template <typename MakeMap>
static void textTransaction(SP<CWLSurfaceResource> surface, SP<IKeyboard> agent,
                            const std::vector<uint32_t> &scalars,
                            MakeMap makeMap) {
  size_t completed = 0;
  try {
    // Validate the entire chunk and allocate its map before any input effects.
    auto map = makeMap(textKeymap(scalars));
    InputTransaction transaction(surface, false);
    transaction.borrowKeyboard(agent);
    transaction.installTargetKeymap(map);
    for (size_t i = 0; i < scalars.size(); ++i) {
      transaction.key(TEXT_KEY_CODES[i], 0);
      ++completed;
    }
    transaction.finish();
  } catch (const std::exception &e) {
    throw TextFailure(e.what(), completed);
  }
}
