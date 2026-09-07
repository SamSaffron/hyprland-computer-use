# Third-party components

- `native/virtual-keyboard-unstable-v1.xml` was obtained from [atx/wtype](https://github.com/atx/wtype/blob/master/protocol/virtual-keyboard-unstable-v1.xml). Its copyright and permissive license notices are retained verbatim in that file. Generated protocol bindings are build outputs, not committed source.
- `native/wlr-virtual-pointer-unstable-v1.xml` is the wlroots virtual-pointer protocol from [wlr-protocols](https://gitlab.freedesktop.org/wlroots/wlr-protocols). Its original copyright and permissive license are retained verbatim.
- The Go MCP implementation uses the official [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk), pinned in `go.mod` and `go.sum`.
- StatusNotifierItem uses [godbus/dbus](https://github.com/godbus/dbus), likewise pinned.
- Hyprland, Quickshell, Wayland, libxkbcommon, grim and ffmpeg are external build/runtime dependencies, not relicensed by this repository. The compositor plugin must be built against a compatible Hyprland installation.
- Demo/reference photos and generated videos are not included in this source repository.
