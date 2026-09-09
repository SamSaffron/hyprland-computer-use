// Pure authority bookkeeping shared by the independent seat and its tests.
// A Wayland serial is useful only within the lease/root that produced it.
#pragma once
#include <chrono>
#include <cstdint>
#include <deque>
#include <string>

class SeatSerials {
public:
  using Time = std::chrono::steady_clock::time_point;
  void issue(uint32_t serial, const std::string &lease, uintptr_t root,
             Time until) {
    if (entries.size() == 128)
      entries.pop_front();
    entries.push_back({serial, lease, root, until});
  }
  bool consume(uint32_t serial, const std::string &lease, uintptr_t root,
               Time now) {
    for (auto it = entries.begin(); it != entries.end(); ++it) {
      if (it->serial != serial)
        continue;
      const bool valid =
          it->lease == lease && it->root == root && now < it->until;
      entries.erase(
          it); // never allow replay, including after a rejected attempt
      return valid;
    }
    return false;
  }
  void clear() { entries.clear(); }

private:
  struct Entry {
    uint32_t serial;
    std::string lease;
    uintptr_t root;
    Time until;
  };
  std::deque<Entry> entries;
};
