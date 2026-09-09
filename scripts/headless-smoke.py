#!/usr/bin/env python3
"""Disposable headless integration test; never connects to the caller's desktop.

Requires Hyprland, matching built plugins, Mesa and Kitty. Failure to initialize
headless rendering is a test failure, not a skipped security check.
"""
import json
import os
from pathlib import Path
import socket
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
BINARY = ROOT / "build/hyprland-computer-use"


def wait_for(predicate, seconds=30):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(0.1)
    raise RuntimeError("timed out waiting for disposable compositor/test")


def exchange(path, command):
    with socket.socket(socket.AF_UNIX) as conn:
        conn.settimeout(10)
        conn.connect(str(path))
        conn.sendall(json.dumps(command).encode() + b"\n")
        return json.loads(conn.makefile().readline())


def worker(directory):
    processes = []
    directory = Path(directory)
    ui = None
    try:
        subprocess.run(["hyprctl", "output", "create", "headless"], check=True)
        subprocess.run(["hyprctl", "keyword", "monitor", "HEADLESS-1,1280x720@60,0x0,1"], check=True)
        # Load only into this newly created, disposable compositor.
        subprocess.run(["hyprctl", "plugin", "load", str(ROOT / "build/guard.so")], check=True)
        processes.append(subprocess.Popen([str(BINARY), "serve", "--data", str(directory / "data")]))
        ui_path = directory / "computer-use/ui.sock"
        wait_for(ui_path.exists)
        ui = socket.socket(socket.AF_UNIX)
        ui.settimeout(10)
        ui.connect(str(ui_path))
        stream = ui.makefile("r")

        def local(command):
            ui.sendall(json.dumps(command).encode() + b"\n")
            state = json.loads(stream.readline())
            assert not state.get("error"), state
            return state

        local({"op": "pause", "paused": False})
        processes.append(subprocess.Popen(["kitty", "--class", "smoke-target", "sh", "-c", "cat > /dev/null"]))
        probe = directory / "probe.sock"
        processes.append(subprocess.Popen([sys.executable, str(ROOT / "scripts/mcp-probe.py"),
                                          "--binary", str(BINARY), "--socket", str(probe),
                                          "--trace", str(directory / "mcp.jsonl")]))
        wait_for(probe.exists)

        def call(tool, args=None):
            local({"op": "state"})  # Keep the simulated local supervisor alive.
            reply = exchange(probe, {"tool": tool, "args": args or {}})
            assert "error" not in reply, reply
            result = reply["result"]
            assert not result.get("isError"), result
            return result.get("structuredContent") or json.loads(next(c["text"] for c in result["content"] if c["type"] == "text"))

        target = wait_for(lambda: next((w for w in call("list_windows")["windows"]
                                       if w["class"] == "smoke-target"), None))
        args = {"window_id": target["id"], "revision": target["revision"],
                "actions": [{"type": "key", "key": "A"}]}
        request = call("input_window", args)
        assert request["status"] == "approval_required", request
        state = local({"op": "approve", "id": request["request_id"], "seconds": 60})
        grant = next(g for g in state["grants"] if g["capability"] == "control")
        assert call("input_window", args)["status"] == "completed"
        lease_probe = {"op": "release", "token": grant["id"]}
        assert exchange(directory / "computer-use/guard.sock", lease_probe)["ok"]
        state = local({"op": "revoke", "id": grant["id"]})
        assert not state["revocation_unconfirmed"], state
        assert call("input_window", args)["status"] == "approval_required"
        # Exercise guard.cpp directly: the old token must also fail at the plugin.
        reply = exchange(directory / "computer-use/guard.sock",
                         lease_probe)
        assert not reply["ok"] and reply.get("error") == "lease_expired_or_revoked", reply
        local({"op": "pause", "paused": True})
        (directory / "passed").write_text("request -> approve -> input -> revoke -> denied\n")
    finally:
        if ui:
            ui.close()
        for process in reversed(processes):
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def main():
    if len(sys.argv) == 3 and sys.argv[1] == "--worker":
        worker(sys.argv[2])
        return
    with tempfile.TemporaryDirectory(prefix="cu-headless-") as temp:
        directory = Path(temp)
        env = dict(os.environ)
        for key in ("WAYLAND_DISPLAY", "WAYLAND_SOCKET", "DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE"):
            env.pop(key, None)
        env.update(XDG_RUNTIME_DIR=temp, XDG_CACHE_HOME=str(directory / "cache"),
                   XDG_DATA_HOME=str(directory / "data"), AQ_BACKEND="headless", LIBGL_ALWAYS_SOFTWARE="1")
        config = directory / "hyprland.conf"
        config.write_text("misc {\n disable_hyprland_logo = true\n disable_splash_rendering = true\n}\n"
                          f"exec-once = {sys.executable} {Path(__file__).resolve()} --worker {temp}\n")
        with (directory / "compositor.log").open("w+") as log:
            process = subprocess.Popen(["Hyprland", "--config", str(config)], env=env, stdout=log, stderr=log, start_new_session=True)
            try:
                def done():
                    if process.poll() is not None:
                        raise RuntimeError("headless compositor exited before smoke completed")
                    return (directory / "passed").exists()
                wait_for(done, 120)
                print((directory / "passed").read_text())
            finally:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
                log.seek(0)
                print(log.read())


if __name__ == "__main__":
    main()
