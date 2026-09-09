package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the production model-update and delegate-binding snippets with real
// Quickshell Variants, but plain QtObjects instead of compositor surfaces.
// Offscreen Qt + a private bus/runtime never opens the desktop permission UI.
func TestOutlineDelegateStability(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_QML") != "1" {
		t.Skip("set COMPUTER_USE_TEST_QML=1 for offscreen Quickshell lifecycle test")
	}
	source, err := consoleBundle.ReadFile("quickshell/shell.qml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(source)
	start := strings.Index(s, "let s=JSON.parse(data); root.state=s;")
	if start < 0 {
		t.Fatal("state update not found")
	}
	end := strings.Index(s[start:], "if(s.error)")
	if end < 0 {
		t.Fatal("state update end not found")
	}
	update := strings.Replace(s[start:start+end], "let s=JSON.parse(data); ", "", 1)
	start = strings.Index(s, "    Variants {\n        model: root.targetIDs")
	if start < 0 {
		t.Fatal("outline must use stable IDs")
	}
	end = strings.Index(s[start:], "            screen:")
	if end < 0 {
		t.Fatal("outline delegate bindings not found")
	}
	delegate := strings.Replace(s[start:start+end], "PanelWindow {", "QtObject {", 1)
	markerStart := strings.Index(s, "text:target.label")
	if markerStart < 0 {
		t.Fatal("marker text binding not found")
	}
	markerEnd := strings.Index(s[markerStart:], ";color:")
	if markerEnd < 0 {
		t.Fatal("marker text end not found")
	}
	markerExpression := strings.TrimPrefix(s[markerStart:markerStart+markerEnd], "text:")
	qml := `import QtQuick
import Quickshell
ShellRoot {
 id: root
 property var state: ({targets:[]})
 property var targetIDs: []
 property int created: 0
 property int destroyed: 0
 property int latest: 0
 property string latestLabel: ""
 property int step: 0
 function apply(s) { ` + update + ` }
 function mark(seconds,x,mode) { return {targets:[{window:{id:"instance-A",at:[x,0],size:[400,300]},label:"CONTROL GRANTED",input_mode:mode||"Seat",remaining_seconds:seconds}]}; }
 function check(ok,message) { if(!ok) { console.error(message); Qt.exit(1); } }
 ` + delegate + `
  property string markerLabel: ` + markerExpression + `
  onMarkerLabelChanged: root.latestLabel=markerLabel
  Component.onCompleted: root.created++
  Component.onDestruction: root.destroyed++
  onTargetChanged: root.latest=target.remaining_seconds
 } }
 Timer { interval:50;running:true;repeat:true;onTriggered: {
  switch(root.step++) {
  case 0: root.apply(root.mark(300,0)); break;
  case 1: root.check(root.created===1 && root.destroyed===0 && root.latestLabel.indexOf("Seat")>=0,"initial seat label"); root.apply(root.mark(299,0,"Fallback")); break;
  case 2: root.check(root.created===1 && root.destroyed===0 && root.latest===299 && root.latestLabel.indexOf("Fallback")>=0,"fallback label or countdown recreated outline or failed to update"); root.apply(root.mark(298,20)); break;
  case 3: root.check(root.created===1 && root.destroyed===0 && root.latest===298,"geometry recreated outline"); root.apply({targets:[]}); break;
  case 4: root.check(root.destroyed===1,"revocation retained outline"); console.log("OUTLINE_STABLE"); Qt.quit(); break;
  }
 } }
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "shell.qml")
	if err := os.WriteFile(path, []byte(qml), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dbus-run-session", "--", "qs", "--no-color", "-p", path)
	cmd.Env = append(os.Environ(), "QT_QPA_PLATFORM=offscreen", "XDG_RUNTIME_DIR="+dir, "XDG_CACHE_HOME="+dir, "WAYLAND_DISPLAY=", "WAYLAND_SOCKET=", "HYPRLAND_INSTANCE_SIGNATURE=")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "OUTLINE_STABLE") {
		t.Fatalf("outline lifecycle: %v\n%s", err, out)
	}
	t.Log(string(out))
}
