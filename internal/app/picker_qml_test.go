package app

import (
	"os"
	"strings"
	"testing"
)

func TestPickerUsesEveryScreenAndGlobalIntersection(t *testing.T) {
	raw, err := os.ReadFile("../../quickshell/Picker.qml")
	if err != nil {
		t.Fatal(err)
	}
	qml := string(raw)
	for _, required := range []string{
		"Variants {",
		"model: Quickshell.screens",
		"screen: modelData",
		"globalX < panel.screen.x + panel.screen.width",
		"globalRight > panel.screen.x",
		"Math.max(globalX, panel.screen.x) - panel.screen.x",
	} {
		if !strings.Contains(qml, required) {
			t.Fatalf("picker is missing multi-monitor behavior %q", required)
		}
	}
	if strings.Contains(qml, "screen: Quickshell.screens[0]\n") {
		t.Fatal("picker panel is still pinned to the first screen")
	}
}

func TestActionButtonsHaveKeyboardAndAccessibilityMetadata(t *testing.T) {
	raw, err := os.ReadFile("../../quickshell/shell.qml")
	if err != nil {
		t.Fatal(err)
	}
	qml := string(raw)
	for _, required := range []string{"activeFocusOnTab: enabled", "Accessible.name: label", "Keys.onReturnPressed", "Keys.onSpacePressed"} {
		if !strings.Contains(qml, required) {
			t.Fatalf("action button is missing %q", required)
		}
	}
}
