// Unit-test the production selector/hit-test/path planner without Hyprland.
#include <cassert>
#include <cstdint>
#include <iostream>
#include <limits>
#include <memory>
#include <utility>
template <class T> using SP = std::shared_ptr<T>;
template <class T> using WP = std::weak_ptr<T>;
struct CWLSurfaceResource {};
struct Vector2D {
  double x = 0, y = 0;
  Vector2D operator+(Vector2D b) const { return {x + b.x, y + b.y}; }
  Vector2D operator-(Vector2D b) const { return {x - b.x, y - b.y}; }
  bool operator==(const Vector2D &) const = default;
};
#include "surface_routing.hpp"
template <class F> static void refused(F call, const std::string &error) {
  try {
    call();
    assert(false);
  } catch (const std::runtime_error &e) {
    assert(e.what() == error);
  }
}
int main() {
  auto root = std::make_shared<CWLSurfaceResource>();
  auto below = std::make_shared<CWLSurfaceResource>();
  auto above = std::make_shared<CWLSurfaceResource>();
  auto nested = std::make_shared<CWLSurfaceResource>();
  auto popup = std::make_shared<CWLSurfaceResource>();
  auto popupChild = std::make_shared<CWLSurfaceResource>();
  std::vector<SurfaceNode> nodes = {
      {below, {5, 5}, {40, 40}, "subsurface", root},
      {root, {0, 0}, {100, 100}, "toplevel", nullptr},
      {above, {20, 30}, {40, 40}, "subsurface", root},
      {nested, {25, 35}, {10, 10}, "subsurface", above},
      {popup, {-50, 100}, {80, 60}, "popup", nullptr},
      {popupChild, {-45, 105}, {20, 20}, "subsurface", popup}};
  auto all = [](auto, Vector2D) { return true; };
  const auto rootNode = nodes[1], popupNode = nodes[4];
  auto hit = [&](Vector2D p) {
    return hitSurfaceTree(nodes, rootNode, p, all);
  };
  assert(hit({10, 10}).first == root); // below-parent child does not steal hits
  auto sub = hit({22, 32});
  assert(sub.first == above && (sub.second == Vector2D{2, 2}));
  auto deep = hit({27, 38});
  assert(deep.first == nested && (deep.second == Vector2D{2, 3}));
  auto hole = hitSurfaceTree(nodes, rootNode, {10, 10},
                             [&](auto s, Vector2D) { return s != root; });
  assert(hole.first == below && (hole.second == Vector2D{5, 5}));
  assert(!hitSurfaceTree(nodes, rootNode, {0, 120}, all)
              .first); // no implicit popup routing
  auto pop = hitSurfaceTree(nodes, popupNode, {7, 8}, all);
  assert(pop.first == popupChild && (pop.second == Vector2D{2, 3}));
  auto poly = planSurfacePolyline(nodes, {100, 100},
                                  {{65, 70}, {85, 90}, {65, 90}}, hit);
  assert(poly.surface == root && poly.points.size() == 41);
  assert((poly.points[20] == Vector2D{85, 90}));
  refused([&] { planSurfacePolyline(nodes, {100, 100}, {{65, 70}}, hit); },
          "invalid_drag_path_size");
  refused(
      [&] {
        planSurfacePolyline(nodes, {100, 100}, {{65, 70}, {105, 70}}, hit);
      },
      "outside_surface");
  refused(
      [&] {
        planSurfacePolyline(nodes, {100, 100}, {{65, 70}, {27, 38}, {65, 90}},
                            hit);
      },
      "cross_surface_drag_unsupported");
  auto path = planSurfacePath(nodes, {100, 100}, {50, 50}, {55, 55}, true, hit);
  assert(path.surface == above && path.points.size() == 21);
  assert((path.points.front() == Vector2D{30, 20}) &&
         (path.points.back() == Vector2D{35, 25}));
  refused(
      [&] { planSurfacePath(nodes, {100, 100}, {0, 0}, {50, 50}, true, hit); },
      "cross_surface_drag_unsupported");
  refused(
      [&] { planSurfacePath(nodes, {100, 100}, {-1, 0}, {0, 0}, false, hit); },
      "outside_surface");
  refused(
      [&] { planSurfacePath(nodes, {100, 100}, {100, 0}, {0, 0}, false, hit); },
      "outside_surface");
  refused(
      [&] {
        planSurfacePath(nodes, {100, 100},
                        {std::numeric_limits<double>::quiet_NaN(), 0}, {0, 0},
                        false, hit);
      },
      "outside_surface");
  auto foreign = std::make_shared<CWLSurfaceResource>();
  refused(
      [&] {
        planSurfacePath(nodes, {100, 100}, {1, 1}, {1, 1}, false, [&](auto) {
          return std::pair{foreign, Vector2D{1, 1}};
        });
      },
      "surface_input_region_refused");
  SurfaceHandles handles("epoch");
  const auto id = handles.identify(root, popup);
  assert(handles.identify(root, popup) == id);
  assert(handles.resolve(id, root, nodes) == popup);
  refused([&] { handles.resolve(id, foreign, nodes); },
          "surface_unavailable_rediscover");
  auto closed = nodes;
  std::erase_if(closed, [&](const auto &n) { return n.surface == popup; });
  refused([&] { handles.resolve(id, root, closed); },
          "surface_unavailable_rediscover");
  SurfaceHandles reloaded("new-epoch");
  assert(reloaded.identify(root, popup) != id);
  refused([&] { reloaded.resolve(id, root, nodes); },
          "surface_unavailable_rediscover");
  std::string dead;
  {
    auto temporary = std::make_shared<CWLSurfaceResource>();
    dead = handles.identify(root, temporary);
  }
  auto replacement = std::make_shared<CWLSurfaceResource>();
  assert(handles.identify(root, replacement) != dead);
  refused([&] { handles.resolve(dead, root, nodes); },
          "surface_unavailable_rediscover");
  std::cout << "surface identity, stacking, offsets, bounds and drag planning "
               "tests passed\n";
}
