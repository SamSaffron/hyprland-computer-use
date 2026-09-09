// Temporary, read-only setup helper. Hyprland 0.56.2's plugin list omits
// filenames, so inspect the actual plugin objects instead of guessing paths.
#include "popup_hook.hpp"
#include <filesystem>
#include <hyprland/src/plugins/PluginAPI.hpp>
#include <hyprland/src/plugins/PluginSystem.hpp>
#include <nlohmann/json.hpp>
#include <unistd.h>

APICALL EXPORT std::string PLUGIN_API_VERSION() { return HYPRLAND_API_VERSION; }
APICALL EXPORT PLUGIN_DESCRIPTION_INFO PLUGIN_INIT(HANDLE h) {
  if (HyprlandAPI::getHyprlandVersion(h).hash != GIT_COMMIT_HASH)
    throw std::runtime_error(
        "setup inspector requires matching Hyprland headers");
  const auto self = g_pPluginSystem->getPluginByHandle(h);
  if (!self)
    throw std::runtime_error("setup inspector not registered");
  const auto path = self->m_path;
  const auto name = "computer-use-setup-inspect-" + std::filesystem::path(path)
                                                        .parent_path()
                                                        .parent_path()
                                                        .filename()
                                                        .string();
  if (!HyprlandAPI::registerHyprCtlCommand(
          h, {name, true, [path](auto, auto) {
                nlohmann::json guards = nlohmann::json::array();
                for (const auto p : g_pPluginSystem->getAllPlugins()) {
                  if (p->m_name == "computer-use-guard" &&
                      p->m_author == "Computer Use")
                    guards.push_back({{"path", p->m_path},
                                      {"configured", p->m_loadedWithConfig},
                                      {"restart_required", p->m_version.ends_with("-independent-seat")}});
                }
                return nlohmann::json{
                    {"inspector", path}, {"pid", getpid()}, {"guards", guards},
                    {"independent_seat_hook_available", independentPopupGrabAddress() != nullptr}}
                    .dump();
              }}))
    throw std::runtime_error("setup inspector command registration failed");
  return {name, "Temporary read-only Computer Use setup inspection",
          "Computer Use", "1"};
}
APICALL EXPORT void PLUGIN_EXIT() {}
