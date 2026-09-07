import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import Quickshell.Widgets
import Quickshell.Wayland
import Quickshell.Services.SystemTray

ShellRoot {
    id: root
    property var state: ({mode:"approve", paused:false, requests:[], grants:[], audit:[], recordings:[], open:0})
    property bool openPanel: true
    property string lastError: ""
    property bool confirmYolo: false
    property int lastOpen: -1
    property int minutes: 5
    property bool demoTray: Quickshell.env("COMPUTER_USE_DEMO_TRAY") === "1"
    property color ink: "#e9eff4"
    property color muted: "#9baebb"
    property color accent: state.mode === "yolo" ? "#ed8a71" : "#65d7b2"
    function send(op, fields) {
        if(op!=="state") root.lastError="";
        let q = fields || {}; q.op=op;
        control.write(JSON.stringify(q)+"\n"); control.flush();
    }
    Socket {
        id: control
        path: Quickshell.env("XDG_RUNTIME_DIR")+"/computer-use/ui.sock"
        connected: true
        parser: SplitParser {
            onRead: data => {
                try {
                    let s=JSON.parse(data); root.state=s;
                    if(s.error) root.lastError=s.error;
                    if(s.open!==root.lastOpen) { root.lastOpen=s.open; root.openPanel=true; }
                } catch(e) { console.warn("Invalid broker state", e); }
            }
        }
        onConnectedChanged: if(connected) root.send("state")
    }
    Timer { interval: 400; running: true; repeat: true; onTriggered: { if(control.connected) root.send("state"); else control.connected=true; } }
    component ActionButton: Rectangle {
        id: button
        property string label
        property color tint: "#263b49"
        property color foreground: root.ink
        signal clicked()
        implicitWidth: labelText.implicitWidth+26
        implicitHeight: 36
        radius: 8
        color: mouse.containsMouse ? Qt.lighter(tint,1.2) : tint
        Text { id: labelText; anchors.centerIn: parent; text: button.label; textFormat: Text.PlainText; color: button.foreground; font.pixelSize: 13; font.bold: true }
        MouseArea { id: mouse; anchors.fill: parent; hoverEnabled: true; onClicked: button.clicked() }
    }
    Picker {
        brokerState:root.state;errorText:root.lastError
        onSelectWindow:(id,client,window)=>root.send("select_share",{id:id,client:client,window_id:window})
        onCancelSelection:id=>root.send("cancel_share",{id:id})
    }
    // Demo host only. In a normal desktop, the exported StatusNotifierItem lives in the existing tray.
    PanelWindow {
        visible: root.demoTray
        screen: Quickshell.screens[0]
        anchors { top:true; right:true }
        margins { top:10; right:12 }
        implicitWidth: 305; implicitHeight: 44
        exclusionMode: ExclusionMode.Ignore
        color: "transparent"
        WlrLayershell.namespace: "computer-use-demo-tray"
        Rectangle { anchors.fill:parent; radius:12; color:"#15232e"; border.color:"#3c5361"
            Row { anchors.centerIn:parent; spacing:10
                Repeater { model: SystemTray.items
                    IconImage {
                        required property var modelData
                        implicitSize: 26
                        source: modelData.icon
                        MouseArea { anchors.fill:parent; onClicked: { parent.modelData.activate(); root.openPanel=true; } }
                    }
                }
                Text { text: "COMPUTER USE"; color:root.ink; font.pixelSize:13; font.bold:true; anchors.verticalCenter:parent.verticalCenter }
                Rectangle { width:9;height:9;radius:5;color:control.connected?root.accent:"#a25959";anchors.verticalCenter:parent.verticalCenter }
                Text { text: root.state.paused ? "PAUSED" : root.state.mode.toUpperCase();color:root.accent;font.pixelSize:11;anchors.verticalCenter:parent.verticalCenter }
            }
            MouseArea { anchors.fill:parent; onClicked:root.openPanel=!root.openPanel; z:-1 }
        }
    }
    Variants {
        model: root.state.targets || []
        PanelWindow {
            required property var modelData
            screen: Quickshell.screens.find(s => modelData.window.at[0]>=s.x && modelData.window.at[0]<s.x+s.width && modelData.window.at[1]>=s.y && modelData.window.at[1]<s.y+s.height) || Quickshell.screens[0]
            anchors { top:true; left:true }
            margins { left:Math.max(0,modelData.window.at[0]-screen.x);top:Math.max(0,modelData.window.at[1]-screen.y) }
            implicitWidth:modelData.window.size[0];implicitHeight:modelData.window.size[1]
            exclusionMode:ExclusionMode.Ignore;color:"transparent";mask:Region {}
            WlrLayershell.namespace:"computer-use-target"
            WlrLayershell.layer:WlrLayer.Overlay
            Rectangle { anchors.fill:parent;color:"transparent";border.width:3;border.color:root.accent;radius:3 }
            Rectangle { anchors.top:parent.top;anchors.horizontalCenter:parent.horizontalCenter;width:markerText.implicitWidth+20;height:23;color:root.accent;radius:4
                Text { id:markerText;anchors.centerIn:parent;text:modelData.label+(modelData.remaining_seconds?" · "+modelData.remaining_seconds+"s":"");color:"#10251e";font.pixelSize:10;font.bold:true;textFormat:Text.PlainText }
            }
        }
    }
    PanelWindow {
        id: panel
        visible: root.openPanel && !root.state.picker
        screen: Quickshell.screens[0]
        anchors { top:true; right:true }
        margins { top:root.demoTray?66:42; right:12 }
        implicitWidth: 440; implicitHeight: 770
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.keyboardFocus: WlrKeyboardFocus.OnDemand
        color: "transparent"
        WlrLayershell.namespace: "computer-use-permissions"
        WlrLayershell.layer: WlrLayer.Overlay
        Rectangle { anchors.fill:parent; radius:16; color:"#111e28"; border.width:1; border.color:"#405565"
            ColumnLayout { anchors.fill:parent; anchors.margins:22; spacing:13
                RowLayout { Layout.fillWidth:true
                    ColumnLayout { spacing:4
                        Text { text:"Computer Use";color:root.ink;font.pixelSize:24;font.bold:true }
                        Text { text:control.connected?"LOCAL PERMISSION CONSOLE":"OFFLINE — NO CONTROL";color:root.muted;font.pixelSize:10;font.letterSpacing:1.3 }
                    }
                    Item { Layout.fillWidth:true }
                    ActionButton { label:"×"; onClicked:root.openPanel=false }
                }
                Rectangle { Layout.fillWidth:true; implicitHeight:1;color:"#30424e" }
                RowLayout { spacing:8
                    ActionButton { label:"Approve";tint:root.state.mode==="approve"?"#255749":"#263b49";onClicked:{root.confirmYolo=false;root.send("mode",{mode:"approve"})} }
                    ActionButton { label:"Share";onClicked:root.send("begin_share",{seconds:root.minutes*60,capability:"control"}) }
                     ActionButton { label:"YOLO";tint:root.state.mode==="yolo"?"#763e32":"#263b49";onClicked:root.confirmYolo=true }
                    Item { Layout.fillWidth:true }
                    ActionButton { label:root.state.paused?"Resume":"Pause";onClicked:root.send("pause",{paused:!root.state.paused}) }
                }
                Rectangle { visible:root.confirmYolo;Layout.fillWidth:true;implicitHeight:88;color:"#4c302a";radius:9
                    ColumnLayout { anchors.fill:parent;anchors.margins:10;spacing:6
                        Text { text:"YOLO allows all exposed tools without prompts.";color:"#ffd6c5";font.pixelSize:12;textFormat:Text.PlainText }
                        ActionButton { label:"Enable YOLO on this desktop";tint:"#95503c";onClicked:{root.send("mode",{mode:"yolo"});root.confirmYolo=false} }
                    }
                }
                Text { Layout.fillWidth:true;wrapMode:Text.WordWrap;text:root.state.mode==="yolo"?"YOLO is active. New tool requests are automatically allowed. Pause still stops everything.":"The MCP client can request access. Only you can grant it. New windows never inherit control.";color:root.muted;font.pixelSize:12 }
                RowLayout { Layout.fillWidth:true
                    Text { text:"Grant duration";color:root.ink;font.pixelSize:13 }
                    Item { Layout.fillWidth:true }
                    SpinBox { from:1;to:60;value:5;editable:true;onValueModified:root.minutes=value;implicitWidth:110 }
                    Text { text:"minutes";color:root.muted;font.pixelSize:12 }
                }
                Text { visible:root.lastError!=="";Layout.fillWidth:true;wrapMode:Text.WordWrap;text:root.lastError;textFormat:Text.PlainText;color:"#ffae93";font.pixelSize:12 }
                ScrollView { id: scroller; Layout.fillWidth:true;Layout.fillHeight:true;clip:true;contentWidth:availableWidth
                    Column { width:scroller.availableWidth;spacing:12
                        Text { text:"REQUESTS  ·  "+root.state.requests.length;color:root.accent;font.pixelSize:11;font.bold:true;font.letterSpacing:1 }
                        Text { visible:root.state.requests.length===0;text:"No requests waiting";color:root.muted;font.pixelSize:13 }
                        Repeater { model:root.state.requests
                            Rectangle { required property var modelData; width:parent.width;implicitHeight:requestColumn.implicitHeight+24;radius:10;color:"#243642";border.color:"#b58c4b"
                                Column { id:requestColumn;anchors.left:parent.left;anchors.right:parent.right;anchors.top:parent.top;anchors.margins:12;spacing:9
                                    Text { width:parent.width;text:modelData.label;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.ink;font.pixelSize:14;font.bold:true }
                                    Text { width:parent.width;text:"Agent reason (untrusted): "+modelData.reason;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.muted;font.pixelSize:12 }
                                    Text { width:parent.width;text:"Client "+modelData.client.slice(0,8);textFormat:Text.PlainText;color:"#839ca9";font.pixelSize:10 }
                                    Row { spacing:8
                                        ActionButton { label:"Grant "+root.minutes+" min";tint:"#28654f";onClicked:root.send("approve",{id:modelData.id,seconds:root.minutes*60}) }
                                        ActionButton { label:"Deny";onClicked:root.send("deny",{id:modelData.id}) }
                                    }
                                }
                            }
                        }
                        Text { text:"ACTIVE GRANTS  ·  "+root.state.grants.length;color:root.accent;font.pixelSize:11;font.bold:true;font.letterSpacing:1 }
                        Repeater { model:root.state.grants
                            Rectangle { required property var modelData;width:parent.width;implicitHeight:grantColumn.implicitHeight+22;radius:10;color:"#1d3531"
                                Column { id:grantColumn;anchors.left:parent.left;anchors.right:parent.right;anchors.top:parent.top;anchors.margins:11;spacing:8
                                    Text { width:parent.width;text:modelData.label;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.ink;font.pixelSize:12 }
                                    Row { spacing:15
                                        Text { text:Math.floor(modelData.remaining_seconds/60)+":"+(modelData.remaining_seconds%60).toString().padStart(2,"0")+" remaining";color:"#8ee0bf";font.pixelSize:14;anchors.verticalCenter:parent.verticalCenter }
                                        ActionButton { label:"Revoke";tint:"#514038";onClicked:root.send("revoke",{id:modelData.id}) }
                                    }
                                }
                            }
                        }
                        Text { text:"RECENT ACTIVITY";color:root.muted;font.pixelSize:11;font.bold:true;font.letterSpacing:1 }
                        Repeater { model:root.state.audit.slice(-5).reverse()
                            Text { required property var modelData;width:parent.width;text:modelData.event+" · "+modelData.detail;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:"#a3b6c0";font.pixelSize:11 }
                        }
                    }
                }
                ActionButton { Layout.fillWidth:true;label:"Revoke all access";tint:"#693d36";onClicked:root.send("revoke_all") }
                Text { Layout.fillWidth:true;wrapMode:Text.WordWrap;text:"Window-scoped input requires the compositor guard. Same-user shell access is outside this permission boundary.";color:"#78909f";font.pixelSize:10 }
            }
        }
    }
}
