# Automatic input: seat first, guarded fallback

## Setup

Follow [Setup](SETUP.md) for installation and backend selection. The
[compatibility table](COMPATIBILITY.md) owns exact-version, application and
restart limitations; [Security](../SECURITY.md) owns the trust contract.

## Execution and the green outline

`computer_status.input_mode` reports `automatic` for the combined guard, `focus-borrowing` for the compatibility guard, or `independent-seat` for an older seat-only guard.

The combined guard selects a path from the live root client's resources before each transaction:

- **Seat:** the client bound both agent-seat keyboard and pointer resources. Input uses the separate seat without borrowing native focus.
- **Fallback:** the client lacks one or both resources. The existing compositor-guarded transaction temporarily borrows/restores native protocol focus. Human input must be idle; held keys/buttons and native grabs can cause refusal.

The green control-grant outline shows **Seat** or **Fallback**. It is keyed by the exact stable window ID and is shown only while that window's workspace is active (or the window is pinned); inactive and unknown workspaces fail closed so the output-level overlay cannot appear over an unrelated window at the same coordinates. It refreshes from live compositor metadata; if the input-mode query fails it says **Unknown**, not a guessed mode. Application identity strings and titles never select a backend or grant authority.

Fallback does not mean global injection. Both paths require the same live window/root lease, visible target, geometry revision, surface selectors and bounds. No failed or partially delivered transaction is retried. Permission denials, stale/foreign surfaces, constraints, IME grabs, invalid popup serials and other safety failures remain errors. An asynchronous menu failure cannot safely be replayed as a fallback click.

The broker creates protocol-only native virtual devices for the guarded fallback, selecting the native seat by its `Hyprland` name rather than registry order. It verifies the Wayland/guard compositor peer before creating devices and stops on compositor loss. Guard-instance IDs prevent authority from surviving a plugin replacement. Old seat-only guards retain their registry-only broker connection.

## Validation

See [Testing](TESTING.md) for automated commands, disposable compositor checks,
and explicit gaps. Backend markers describe protocol selection, not proof of
application completion or an OS sandbox.
