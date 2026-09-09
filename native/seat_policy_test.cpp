#include "seat_policy.hpp"
#include <cassert>
int main() {
  SeatSerials s;
  auto now = SeatSerials::Time{};
  auto later = now + std::chrono::seconds(1);
  s.issue(1, "lease-A", 10, later);
  assert(!s.consume(1, "lease-B", 10, now));
  assert(!s.consume(1, "lease-A", 10, now));
  s.issue(2, "lease-A", 10, later);
  assert(!s.consume(2, "lease-A", 20, now));
  s.issue(3, "lease-A", 10, later);
  assert(!s.consume(3, "lease-A", 10, later));
  s.issue(4, "lease-A", 10, later);
  assert(s.consume(4, "lease-A", 10, now));
  assert(!s.consume(4, "lease-A", 10, now));
  s.issue(5, "lease-A", 10, later);
  s.clear();
  assert(!s.consume(5, "lease-A", 10, now));
  for (unsigned i = 0; i < 129; ++i)
    s.issue(i, "lease-A", 10, later);
  assert(!s.consume(0, "lease-A", 10, now));
  assert(s.consume(128, "lease-A", 10, now));
}
