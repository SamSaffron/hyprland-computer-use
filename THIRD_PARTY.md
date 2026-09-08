# Third-party components

- `native/virtual-keyboard-unstable-v1.xml` was obtained from [atx/wtype](https://github.com/atx/wtype/blob/master/protocol/virtual-keyboard-unstable-v1.xml). Its copyright and permissive license notices are retained verbatim in that file. The built-in Go Wayland client implements the device-creation/keymap subset; no generated C bindings are required.
- `native/wlr-virtual-pointer-unstable-v1.xml` is the wlroots virtual-pointer protocol from [wlr-protocols](https://gitlab.freedesktop.org/wlroots/wlr-protocols). Its original copyright and permissive license are retained verbatim.
- The Go MCP implementation uses the official [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk), pinned in `go.mod` and `go.sum`.
- StatusNotifierItem uses [godbus/dbus](https://github.com/godbus/dbus), likewise pinned.
- Hyprland, Quickshell, Wayland, libxkbcommon, libei, grim and ffmpeg are external build/runtime dependencies, not relicensed by this repository. The compositor plugin must be built against a compatible Hyprland installation.
- Demo/reference photos and generated videos are not included in this source repository.

The distributed Go executable embeds the native sources, Makefile, and Quickshell QML. Both Wayland protocol XML files are embedded verbatim, including their original copyright and license notices; `setup` extracts them alongside the native build inputs. Native dependencies and Quickshell are not embedded.

The keyboard/pointer lifecycle is implemented in Go using `golang.org/x/sys/unix` (pinned in `go.mod`) for Unix descriptor passing and sealed memory files. Its self-contained US-ASCII keymap is project code, not an extracted system XKB keymap. The Go executable does not link libwayland-client or libxkbcommon.

The compositor guard includes Hyprland's input-capture API to refuse borrowed input during an active capture. Building that header requires the `libeis-1.0` pkg-config include flags (Arch package `libei`); the Go binary does not link libei.
