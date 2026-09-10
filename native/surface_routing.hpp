// Surface handles are selectors, never authority. The caller must independently
// validate a live root-window lease and current tree membership on every input.
#pragma once
#include <algorithm>
#include <cmath>
#include <map>
#include <stdexcept>
#include <string>
#include <vector>

struct SurfaceNode {
  SP<CWLSurfaceResource> surface;
  Vector2D offset, size;
  std::string kind;
  SP<CWLSurfaceResource>
      parent; // subsurface parent; popup bases are separate trees
};
static bool surfacePointInside(const Vector2D &point, const Vector2D &size) {
  return std::isfinite(point.x) && std::isfinite(point.y) && point.x >= 0 &&
         point.y >= 0 && point.x < size.x && point.y < size.y;
}
static bool treeContains(const std::vector<SurfaceNode> &nodes,
                         SP<CWLSurfaceResource> surface) {
  return surface && std::ranges::any_of(nodes, [&](const auto &node) {
           return node.surface == surface;
         });
}

static bool surfaceDescends(const std::vector<SurfaceNode> &nodes,
                            SP<CWLSurfaceResource> child,
                            SP<CWLSurfaceResource> ancestor) {
  for (size_t depth = 0; child && depth <= nodes.size(); ++depth) {
    if (child == ancestor)
      return true;
    auto it = std::ranges::find_if(
        nodes, [&](const auto &n) { return n.surface == child; });
    if (it == nodes.end())
      return false;
    child = it->parent;
  }
  return false;
}
// Nodes are in protocol stacking order (below children, parent, above
// children). Only the explicitly selected subsurface tree participates, not
// other popups.
template <typename Accepts>
static std::pair<SP<CWLSurfaceResource>, Vector2D>
hitSurfaceTree(const std::vector<SurfaceNode> &nodes,
               const SurfaceNode &selected, Vector2D point, Accepts accepts) {
  for (auto it = nodes.rbegin(); it != nodes.rend(); ++it) {
    if (!surfaceDescends(nodes, it->surface, selected.surface))
      continue;
    const auto local = selected.offset + point - it->offset;
    if (surfacePointInside(local, it->size) && accepts(it->surface, local))
      return {it->surface, local};
  }
  return {nullptr, {}};
}

// A drag is one bounded transaction on ONE actual surface. Resolve every path
// point before pointer-enter/press, and reject paths crossing a surface
// boundary.
struct SurfacePath {
  SP<CWLSurfaceResource> surface;
  std::vector<Vector2D> points;
};
template <typename Hit>
static SurfacePath planSurfacePath(const std::vector<SurfaceNode> &nodes,
                                   Vector2D size, Vector2D start, Vector2D end,
                                   bool drag, Hit hit) {
  if (!surfacePointInside(start, size) || !surfacePointInside(end, size))
    throw std::runtime_error("outside_surface");
  SurfacePath result;
  const int steps = drag ? 20 : 0;
  for (int i = 0; i <= steps; ++i) {
    const double t = steps ? double(i) / steps : 0;
    auto [surface, local] = hit(Vector2D{start.x + (end.x - start.x) * t,
                                         start.y + (end.y - start.y) * t});
    if (!treeContains(nodes, surface))
      throw std::runtime_error("surface_input_region_refused");
    if (result.surface && result.surface != surface)
      throw std::runtime_error("cross_surface_drag_unsupported");
    result.surface = surface;
    result.points.push_back(local);
  }
  return result;
}

// Each segment uses the same bounded hit-testing as the legacy straight drag.
// Reject ALL segments before entering a surface or pressing a button.
template <typename Hit>
static SurfacePath
planSurfacePolyline(const std::vector<SurfaceNode> &nodes, Vector2D size,
                    const std::vector<Vector2D> &points, Hit hit) {
  if (points.size() < 2 || points.size() > 64)
    throw std::runtime_error("invalid_drag_path_size");
  SurfacePath result;
  for (size_t i = 1; i < points.size(); ++i) {
    auto segment =
        planSurfacePath(nodes, size, points[i - 1], points[i], true, hit);
    if (result.surface && result.surface != segment.surface)
      throw std::runtime_error("cross_surface_drag_unsupported");
    result.surface = segment.surface;
    result.points.insert(result.points.end(),
                         segment.points.begin() + (i > 1 ? 1 : 0),
                         segment.points.end());
  }
  return result;
}

struct SurfaceHandle {
  WP<CWLSurfaceResource> root, surface;
};
class SurfaceHandles {
  std::map<std::string, SurfaceHandle> entries;
  std::string epoch;
  uint64_t next = 0;

public:
  explicit SurfaceHandles(std::string prefix) : epoch(std::move(prefix)) {}
  std::string identify(SP<CWLSurfaceResource> root,
                       SP<CWLSurfaceResource> surface) {
    std::erase_if(entries, [](const auto &item) {
      return !item.second.root.lock() || !item.second.surface.lock();
    });
    for (const auto &[id, entry] : entries)
      if (entry.root.lock() == root && entry.surface.lock() == surface)
        return id;
    if (entries.size() >= 4096)
      throw std::runtime_error("surface_handle_limit");
    const auto id = epoch + "-" + std::to_string(++next);
    entries.emplace(id, SurfaceHandle{root, surface});
    return id;
  }
  SP<CWLSurfaceResource>
  resolve(const std::string &id, SP<CWLSurfaceResource> root,
          const std::vector<SurfaceNode> &current) const {
    auto it = entries.find(id);
    if (it == entries.end() || it->second.root.lock() != root ||
        !treeContains(current, it->second.surface.lock()))
      throw std::runtime_error("surface_unavailable_rediscover");
    return it->second.surface.lock();
  }
};
