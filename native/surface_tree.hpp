// Hyprland 0.56.2 adapter. All discovery starts from the authorized root's
// compositor-owned trees, never a PID/title search or a desktop hit-test.
#pragma once
#include "surface_routing.hpp"
#include <functional>
#include <hyprland/src/protocols/XDGShell.hpp>
#include <hyprland/src/protocols/core/Subcompositor.hpp>
#include <random>
#include <set>

static std::string surfaceEpoch() {
  std::random_device random;
  return std::format("s{:08x}{:08x}{:08x}{:08x}", random(), random(), random(),
                     random());
}
static SurfaceHandles surfaceHandles(surfaceEpoch());
static constexpr size_t MAX_WINDOW_SURFACES = 128;

static bool mappedPopupAncestry(SP<CWLSurfaceResource> surface,
                                SP<CXDGSurfaceResource> root) {
  if (!surface || !surface->m_role ||
      surface->m_role->role() != SURFACE_ROLE_XDG_SHELL)
    return false;
  auto role = dynamicPointerCast<CXDGSurfaceRole>(surface->m_role);
  auto xdg = role ? role->m_xdgSurface.lock() : nullptr;
  for (size_t depth = 0; xdg && depth <= 32; ++depth) {
    if (!xdg->m_mapped)
      return false;
    if (xdg == root)
      return true;
    auto popup = xdg->m_popup.lock();
    xdg = popup ? popup->m_parent.lock() : nullptr;
  }
  return false;
}

static std::vector<SurfaceNode> windowSurfaceTree(PHLWINDOW window) {
  std::vector<SurfaceNode> nodes;
  size_t visited = 0;
  const auto root = window->resource();
  if (!root)
    throw std::runtime_error("window_surface_unavailable");
  const auto origin = window->getWindowMainSurfaceBox().pos();
  std::set<CWLSurfaceResource *> seen;
  std::function<void(SP<CWLSurfaceResource>, Vector2D, const std::string &,
                     SP<CWLSurfaceResource>, size_t)>
      walk;
  walk = [&](SP<CWLSurfaceResource> child, Vector2D pos,
             const std::string &kind, SP<CWLSurfaceResource> parent,
             size_t depth) {
    if (++visited > MAX_WINDOW_SURFACES || depth > 32)
      throw std::runtime_error("surface_tree_too_large");
    if (!child || !child->m_mapped || !child->good())
      return;
    if (child->client() != root->client())
      throw std::runtime_error("surface_client_mismatch");
    if (!seen.insert(child.get()).second)
      throw std::runtime_error("surface_tree_ambiguous");
    const auto size = child->m_current.size;
    if (!std::isfinite(pos.x) || !std::isfinite(pos.y) ||
        !std::isfinite(size.x) || !std::isfinite(size.y) || size.x <= 0 ||
        size.y <= 0)
      throw std::runtime_error("invalid_surface_geometry");
    if (child->m_subsurfaces.size() > MAX_WINDOW_SURFACES)
      throw std::runtime_error("surface_tree_too_large");
    auto children = [&](bool above) {
      for (const auto &ref : child->m_subsurfaces) {
        auto sub = ref.lock();
        if (!sub || (sub->m_zIndex >= 0) != above)
          continue;
        if (sub->m_parent.lock() != child)
          throw std::runtime_error("subsurface_parent_mismatch");
        walk(sub->m_surface.lock(), pos + sub->m_position, "subsurface", child,
             depth + 1);
      }
    };
    children(false);
    nodes.push_back({child, pos, size, kind, parent});
    children(true);
  };
  auto addTree = [&](SP<CWLSurfaceResource> surface, Vector2D position,
                     const std::string &kind) {
    walk(surface, position, kind, nullptr, 0);
  };
  addTree(root, {}, "toplevel");
  if (!treeContains(nodes, root))
    throw std::runtime_error("window_surface_unavailable");
  if (window->m_popupHead) {
    size_t popups = 0;
    window->m_popupHead->breadthfirst(
        [&](SP<Desktop::View::CPopup> popup, void *) {
          if (++popups > MAX_WINDOW_SURFACES)
            throw std::runtime_error("surface_tree_too_large");
          if (!popup || !popup->visible() || popup->inert() ||
              !popup->resource())
            return;
          if (!mappedPopupAncestry(popup->resource(),
                                   window->m_xdgSurface.lock()))
            return;
          const auto owner = popup->getT1Owner();
          if (!owner || owner->resource() != root)
            throw std::runtime_error("popup_owner_mismatch");
          addTree(popup->resource(), popup->coordsGlobal() - origin, "popup");
        },
        nullptr);
  }
  return nodes;
}
static std::string surfaceRevision(const SurfaceNode &node) {
  return std::format("{},{},{},{}", node.offset.x, node.offset.y, node.size.x,
                     node.size.y);
}
static SurfaceNode selectWindowSurface(PHLWINDOW window,
                                       const std::vector<SurfaceNode> &nodes,
                                       const json &request) {
  const auto id = request.value("surface_id", "");
  if (id.empty() && !request.value("surface_revision", "").empty())
    throw std::runtime_error("invalid_surface_selector");
  auto selected = id.empty()
                      ? window->resource()
                      : surfaceHandles.resolve(id, window->resource(), nodes);
  for (const auto &node : nodes) {
    if (node.surface != selected)
      continue;
    if (!id.empty() &&
        request.value("surface_revision", "") != surfaceRevision(node))
      throw std::runtime_error("stale_surface_geometry");
    return node;
  }
  throw std::runtime_error("surface_unavailable_rediscover");
}
static PHLWINDOW transientParent(PHLWINDOW window) {
  if (window->m_isX11 || !window->m_xdgSurface ||
      !window->m_xdgSurface->m_toplevel)
    return nullptr;
  auto parent = window->m_xdgSurface->m_toplevel->m_parent.lock();
  return parent ? parent->m_window.lock() : nullptr;
}
static json windowState(PHLWINDOW window) {
  const auto nodes = windowSurfaceTree(window);
  json surfaces = json::array(), related = json::array();
  for (const auto &node : nodes) {
    surfaces.push_back({{"surface_id", surfaceHandles.identify(
                                           window->resource(), node.surface)},
                        {"kind", node.kind},
                        {"revision", surfaceRevision(node)},
                        {"offset", {node.offset.x, node.offset.y}},
                        {"size", {node.size.x, node.size.y}}});
  }
  bool truncated = false;
  const auto parent = transientParent(window);
  for (const auto &other : Desktop::windowState()->windows()) {
    if (!other->m_isMapped || other == window)
      continue;
    const bool child = transientParent(other) == window;
    if (other != parent && !child)
      continue;
    if (related.size() >= 64) {
      truncated = true;
      break;
    }
    related.push_back({{"window_id", std::format("{:x}", other->m_stableID)},
                       {"relationship", child ? "transient_child" : "parent"},
                       {"separate_grant_required", true}});
  }
  return {{"window_id", std::format("{:x}", window->m_stableID)},
          {"revision", revision(window)},
          {"surfaces", surfaces},
          {"related_windows", related},
          {"related_windows_truncated", truncated},
          {"seat_grab_active", bool(g_pSeatManager->m_seatGrab)},
          {"session_locked", locked()},
          {"surface_tree_version", 1}};
}
