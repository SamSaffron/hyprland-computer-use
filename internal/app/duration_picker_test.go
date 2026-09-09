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

// Real Qt Settings across separate console processes, isolated from user config.
func TestDurationPickerMemoryQML(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_QML") != "1" {
		t.Skip("set COMPUTER_USE_TEST_QML=1 for offscreen duration tests")
	}
	dir := t.TempDir()
	source, err := consoleBundle.ReadFile("quickshell/DurationPicker.qml")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "DurationPicker.qml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	shell, err := consoleBundle.ReadFile("quickshell/shell.qml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(shell)
	start, end := strings.Index(text, "    function send("), strings.Index(text, "    Socket {")
	if start < 0 || end < start {
		t.Fatal("production send function not found")
	}
	send := text[start:end]
	qml := `import QtQuick
import Quickshell
ShellRoot {
 id: root
 property string lastError: ""
 property var durationPicker: duration
 QtObject { id: control; property int writes: 0; function write(text) { writes++; } function flush() {} }
 ` + send + `
 DurationPicker { id: duration }
 function check(ok,message) { if (!ok) throw new Error(message); }
 function find(item,name) {
  if (item.objectName===name) return item;
  for (let child of item.children || []) { let match=find(child,name); if(match) return match; }
  return null;
 }
 Timer { interval:50;running:true;repeat:false;onTriggered: {
  try {
   let choices=find(duration,"durationChoices"), field=find(duration,"customDurationMinutes");
   check(choices && field,"missing controls");
   check(choices.leftPadding===12 && choices.rightPadding===32,"duration text/arrow padding regressed");
   check(choices.contentItem.text===choices.displayText,"selected duration is not displayed");
   let phase=Quickshell.env("DURATION_TEST_PHASE");
   if (phase==="presets") {
    check(duration.minutes===5 && duration.valid && !duration.custom,"wrong initial default");
    check(choices.count===7 && choices.textAt(6)==="Custom…","bad choices");
    let values=[1,5,10,15,30,60];
    for(let i=0;i<values.length;i++) { choices.currentIndex=i; choices.activated(i); check(duration.minutes===values[i] && duration.valid,"wrong preset"); }
    choices.currentIndex=3; choices.activated(3);
   } else if(phase==="custom") {
    check(duration.minutes===15 && !duration.custom,"preset not remembered after restart");
    choices.currentIndex=6; choices.activated(6);
    check(duration.custom && duration.minutes===15,"custom not seeded from preset");
    field.text="17"; field.textEdited();
    check(duration.valid && duration.minutes===17,"custom value rejected");
    choices.currentIndex=1; choices.activated(1);
    choices.currentIndex=6; choices.activated(6);
    check(duration.minutes===17,"switching presets lost custom value");
    for (let invalid of ["", "0", "61", "2.5", "abc", "1,0"]) {
     field.text=invalid; field.textEdited();
     check(!duration.valid && duration.minutes===0,"invalid duration accepted: "+invalid);
     let before=control.writes;
     root.send("approve",{seconds:0}); root.send("begin_share",{seconds:0});
     check(control.writes===before,"invalid custom value sent a grant request");
     root.send("deny",{id:"test"});
     check(control.writes===before+1,"invalid duration blocked denial");
    }
    // Exit while invalid: last valid custom value must remain on disk.
   } else if(phase==="restored") {
    check(duration.valid && duration.custom && duration.minutes===17,"custom value or mode not restored");
   } else {
    check(duration.valid && !duration.custom && duration.minutes===5,"corrupt settings did not use safe default");
   }
   console.log("DURATION_MEMORY_OK"); Qt.quit();
  } catch(e) { console.error(e); Qt.exit(1); }
 } }
}
`
	path := filepath.Join(dir, "shell.qml")
	if err = os.WriteFile(path, []byte(qml), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(phase string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "dbus-run-session", "--", "qs", "--no-color", "-p", path)
		cmd.WaitDelay = time.Second
		cmd.Env = append(os.Environ(), "QT_QPA_PLATFORM=offscreen", "XDG_RUNTIME_DIR="+dir, "XDG_CACHE_HOME="+dir, "XDG_CONFIG_HOME="+dir, "WAYLAND_DISPLAY=", "WAYLAND_SOCKET=", "HYPRLAND_INSTANCE_SIGNATURE=", "DURATION_TEST_PHASE="+phase)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "DURATION_MEMORY_OK") {
			t.Fatalf("%s: %v\n%s", phase, err, out)
		}
	}
	run("presets")
	run("custom")
	run("restored")
	settings := filepath.Join(dir, "hyprland-computer-use", "console.ini")
	data, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(data), "grantDuration=") {
		t.Fatalf("no persistent duration: %s %v", data, err)
	}
	if err = os.WriteFile(settings, []byte("[PermissionConsole]\ngrantDuration=invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("corrupt")
}
