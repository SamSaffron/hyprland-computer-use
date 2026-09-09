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

The preferred `guard-seat.so` variant links libwayland-server and libxkbcommon, constructs its fixed US keymap from the installed XKB data, and uses its own protocol seat. It does not change the Go executable's linking. See `docs/INDEPENDENT_SEAT.md` for automatic setup and compatibility limits.

## Release tooling

The release helper and its test (`scripts/release.sh`, `scripts/release_test.go`) are adapted from [term-llm](https://github.com/samsaffron/term-llm). The installer and GoReleaser workflow follow the same release pattern. The upstream MIT notice is retained below:

```text
MIT License

Copyright (c) 2025 Sam Saffron

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
