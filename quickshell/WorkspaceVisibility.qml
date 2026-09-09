import QtQuick

QtObject {
    function active(window, workspaces) {
        if (!window || !window.workspace) return false;
        if (window.pinned) return true;
        for (let i=0; i<workspaces.length; i++) {
            let workspace=workspaces[i];
            if (workspace.id===window.workspace.id) return workspace.active;
        }
        // Fail closed: an output-level overlay must never guess that a window
        // is visible when its workspace cannot be matched to compositor state.
        return false;
    }
}
