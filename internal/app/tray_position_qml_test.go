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

func TestTrayPlacementQML(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_QML") != "1" {
		t.Skip("set COMPUTER_USE_TEST_QML=1 for offscreen placement tests")
	}
	dir := t.TempDir()
	source, err := consoleBundle.ReadFile("quickshell/TrayPlacement.qml")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "TrayPlacement.qml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	qml := `import QtQuick
import Quickshell
ShellRoot {
 TrayPlacement { id: placement }
 function check(ok,message) { if(!ok) throw new Error(message); }
 Timer { interval:50;running:true;repeat:false;onTriggered: {
  try {
   let s={name:"main",x:0,y:0,width:1920,height:1080};
   let cases=[
    {x:1915,y:1060,bar:{x:0,y:1040,w:1920,h:40},edge:"bottom",left:1472,top:262},
    {x:1800,y:20,bar:{x:0,y:0,w:1920,h:40},edge:"top",left:1472,top:48},
    {x:20,y:900,bar:{x:0,y:0,w:40,h:1080},edge:"left",left:48,top:302},
    {x:1900,y:900,bar:{x:1880,y:0,w:40,h:1080},edge:"right",left:1432,top:302}
   ];
   for (let a of cases) {
    let g=placement.geometry(s,a,440,770,false);
    check(g.edge===a.edge && g.x===a.left && g.y===a.top, "bad "+a.edge+" placement: "+JSON.stringify(g));
   }
   // At a corner, actual horizontal bar geometry wins over closest-edge guessing.
   check(placement.geometry(s,cases[0],440,770,false).edge==="bottom","corner ambiguity");
   let left={name:"left",x:-1600,y:-200,width:1600,height:900};
   let a={x:-50,y:680,screen:"left",bar:{x:-1600,y:660,w:1600,h:40}};
   check(placement.screenFor([s,left],a)===left,"wrong negative-origin monitor");
   let g=placement.geometry(left,a,440,770,false);
   check(g.x===1152 && g.y===82,"wrong logical monitor offsets");
   check(placement.screenFor([s,left],{x:-300,y:100})===left,"point monitor selection");
   check(placement.screenFor([s],a)===s,"unplugged monitor fallback");
   check(placement.screenFor([],a)===null,"empty monitor set");
   for (let a of [{x:1000,y:1070},{x:1000,y:0},{x:0,y:500},{x:1919,y:500}]) {
    let g=placement.geometry(s,a,440,770,false);
    check(g.x>=8 && g.y>=8 && g.x+g.width<=1912 && g.y+g.height<=1072,"unbounded inferred placement");
   }
   let small={x:0,y:0,width:360,height:600};
   let tiny=placement.geometry(small,null,440,770,false);
   check(tiny.width===344 && tiny.height===584 && tiny.x===8 && tiny.y===8,"small monitor overflow");
   let bottomSmall=placement.geometry(small,{x:330,y:580,bar:{x:0,y:560,w:360,h:40}},440,770,false);
   check(bottomSmall.y===8 && bottomSmall.y+bottomSmall.height===552,"small monitor panel overlaps toolbar");
   let manual=placement.geometry(s,null,440,770,false);
   check(manual.x===1468 && manual.y===42,"manual launch default");
   console.log("TRAY_PLACEMENT_OK"); Qt.quit();
  } catch(e) { console.error(e); Qt.exit(1); }
 } }
}
`
	path := filepath.Join(dir, "shell.qml")
	if err = os.WriteFile(path, []byte(qml), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dbus-run-session", "--", "qs", "--no-color", "-p", path)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "QT_QPA_PLATFORM=offscreen", "XDG_RUNTIME_DIR="+dir, "XDG_CACHE_HOME="+dir, "WAYLAND_DISPLAY=", "WAYLAND_SOCKET=", "HYPRLAND_INSTANCE_SIGNATURE=")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "TRAY_PLACEMENT_OK") {
		t.Fatalf("placement: %v\n%s", err, out)
	}
}
