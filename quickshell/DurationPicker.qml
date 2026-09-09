import QtQuick
import QtCore
import QtQuick.Controls
import QtQuick.Layouts

ColumnLayout {
    id: root
    spacing: 6
    readonly property bool custom: choices.currentIndex === 6
    readonly property bool valid: custom ? (/^[0-9]+$/.test(customMinutes.text) && validMinutes(Number(customMinutes.text))) : choices.currentIndex >= 0 && choices.currentIndex < 6
    readonly property int minutes: !valid ? 0 : custom ? Number(customMinutes.text) : [1,5,10,15,30,60][choices.currentIndex]
    property int lastPreset: 5
    property int lastCustom: 5
    property bool customUsed: false
    property bool initialized: false
    property url settingsLocation: StandardPaths.writableLocation(StandardPaths.ConfigLocation).toString() + "/hyprland-computer-use/console.ini"

    Settings { id: preferences; location: root.settingsLocation; category: "PermissionConsole" }
    function validMinutes(value) { return Number.isInteger(value) && value >= 1 && value <= 60; }
    function remember() {
        if (!initialized || !valid) return;
        if (custom) { lastCustom=minutes; customUsed=true; }
        // Only presentation preferences are saved, never grants, mode or pause.
        preferences.setValue("grantDuration", JSON.stringify({minutes:minutes,custom:custom}));
        preferences.sync();
    }
    Component.onCompleted: {
        try {
            let saved=JSON.parse(preferences.value("grantDuration", ""));
            let index=[1,5,10,15,30,60].indexOf(saved.minutes);
            if (validMinutes(saved.minutes) && typeof saved.custom === "boolean" && (saved.custom || index >= 0)) {
                if (saved.custom) {
                    customMinutes.text=String(saved.minutes);
                    lastCustom=saved.minutes; customUsed=true;
                    choices.currentIndex=6;
                } else { choices.currentIndex=index; lastPreset=saved.minutes; }
            }
        } catch(e) { /* Missing or invalid saved preference: keep the 5-minute default. */ }
        initialized=true;
    }

    ComboBox {
        id: choices
        objectName: "durationChoices"
        Layout.fillWidth: true
        implicitWidth: 170
        implicitHeight: 36
        font.pixelSize: 13
        currentIndex: 1
        model: ["1 minute", "5 minutes", "10 minutes", "15 minutes", "30 minutes", "1 hour", "Custom…"]
        Accessible.name: "Grant duration"
        leftPadding: 12
        rightPadding: 32
        topPadding: 8
        bottomPadding: 8
        contentItem: Text {
            text: choices.displayText
            font: choices.font
            color: "#e9eff4"
            verticalAlignment: Text.AlignVCenter
            elide: Text.ElideRight
        }
        indicator: Text {
            text: "▾"; color: "#9baebb"; font.pixelSize: 14
            anchors.right: parent.right; anchors.rightMargin: 12
            anchors.verticalCenter: parent.verticalCenter
        }
        background: Rectangle {
            radius: 6; color: choices.down ? "#304957" : "#263b49"
            border.color: choices.activeFocus ? "#65d7b2" : "#405565"
        }
        palette.button: "#263b49"
        palette.buttonText: "#e9eff4"
        palette.base: "#15232e"
        palette.window: "#15232e"
        palette.windowText: "#e9eff4"
        palette.text: "#e9eff4"
        palette.highlight: "#28654f"
        palette.highlightedText: "#e9eff4"
        onActivated: {
            if (root.custom) {
                customMinutes.text = String(root.customUsed ? root.lastCustom : root.lastPreset);
                customMinutes.forceActiveFocus();
                customMinutes.selectAll();
            } else root.lastPreset = root.minutes;
            root.remember();
        }
    }
    RowLayout {
        visible: root.custom
        Layout.fillWidth: true
        TextField {
            id: customMinutes
            objectName: "customDurationMinutes"
            Layout.fillWidth: true
            implicitWidth: 90
            implicitHeight: 36
            font.pixelSize: 13
            leftPadding: 12; rightPadding: 12
            selectionColor: "#28654f"
            selectedTextColor: "#e9eff4"
            text: "5"
            selectByMouse: true
            inputMethodHints: Qt.ImhDigitsOnly
            validator: IntValidator { bottom: 1; top: 60; locale: "C" }
            onTextEdited: root.remember()
            Accessible.name: "Custom grant duration in minutes, 1 to 60"
            color: "#e9eff4"
            background: Rectangle { radius: 5; color: "#15232e"; border.color: root.valid ? "#405565" : "#ffae93" }
        }
        Text { text: "min (1–60)"; color: "#9baebb"; font.pixelSize: 12 }
    }
}
