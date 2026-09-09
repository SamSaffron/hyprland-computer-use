# Multi-input demonstration

[Download the video](demo.mp4?raw=true) · [Static preview](demo-poster.webp) · [Back to the README](../README.md)

The README uses a 1280-pixel-wide, 10-fps animated WebP preview so the demonstration plays directly on GitHub without unsupported `<video>` markup or an external host. Viewers requesting reduced motion receive the static poster instead. The downloadable MP4 retains the full 1080p / 30-fps presentation.

The 30-second silent video shows three real windows on one disposable Hyprland desktop:

- **Agent workspace:** a native GNOME Terminal, granted window-scoped control and using the independent **Seat** input path.
- **term-llm:** the actual live console output of `term-llm ask --mcp computer-use-demo`. The model requests permission, observes the target, types commands, and checks the chart through MCP screenshots.
- **Your window:** Mousepad receiving simulated native-seat typing and pointer input while the model operates the other window.

The two terminals are distinct: the MCP agent controls GNOME Terminal, not its own console.

## What was actually recorded

Recorded September 10, 2026, in an isolated Arch Linux Docker lab. The filmed project revision is [`c628842`](https://github.com/SamSaffron/hyprland-computer-use/commit/c628842e8813a9c6f378e4a3ceea875dce16798c), not a claim that the video exercises every change on current main.

Hyprland **0.56.2**, commit `efb50993780079460b0cbed1363e2166a2de1d9f`, ran as an unprivileged desktop user, nested on a GPU-backed headless Sway session. A private Aquamarine 0.15.0 build supplied lab-only nested-protocol and render-format compatibility patches; this was not a stock physical-monitor deployment. GNOME Terminal 3.60.0 was the agent target. The compositor plugin was compiled against matching Hyprland headers.

The model ran through term-llm on the controller and connected to the lab's ordinary stdio MCP bridge over SSH. Its live output was streamed unchanged to the visible console window. This is a live console feed, not a replay or a fabricated tool transcript. The chart was produced by actual model-selected `input_window` calls, not by a separate shell script writing the result into the application.

The human side is deliberately simulated: a native-seat virtual keyboard types the note, and a native-seat pointer moves, selects text, and clicks the real permission controls. The recorded grant is a **real local button click**. Read-only broker state samples check the target, selected backend, and native active window. The human driver does not use the agent's `input_window` tool.

A full combined rehearsal preceded the final take. In the final take, all five model input batches occurred while the human typing process was running; the native active window remained Mousepad and the target reported **Seat**. The final local Pause click and empty-grant state were verified after the work. The polished cut concentrates on simultaneous work rather than including every check.

## Editing and format

- Original final take: approximately **93.27 seconds**, continuous full-desktop capture.
- Published video: **30.00 seconds**, 1920 × 1080, 30 fps, H.264, no audio, approximately 1.4 MB.
- A short opening preview is followed by the approval and work sequence. Waiting is trimmed and longer sections are accelerated; playback-speed labels identify those sections.
- The text-selection segment is retained at real speed. From that segment through the main chart-building sequence, the source footage is continuous, with a labelled speed change.
- Captions and window labels occupy reserved desktop margins. Application results and tool output are not replaced or composited from different takes.

## Limits

This demonstrates independent input in the tested applications, **not universal toolkit compatibility or physical-device validation**. Clients using **Fallback** temporarily borrow native focus and require idle human input; they do not provide the same concurrency shown here. See [input compatibility](INDEPENDENT_SEAT.md).

Window grants are not an OS sandbox. The compositor, broker, local desktop processes, and simulated-human harness run within the trusted lab environment; a controlled terminal has transitive shell authority. Read [the security model](../SECURITY.md).

Only the selected public video, animated preview, and poster are included here. Raw takes, model logs, and disposable test harnesses are not shipped with the project.
