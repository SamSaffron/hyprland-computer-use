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
    property var targetIDs: []
    property bool openPanel: true
    property string lastError: ""
    property bool confirmYolo: false
    property int lastOpen: -1
    readonly property int minutes: durationPicker.minutes
    property bool demoTray: Quickshell.env("COMPUTER_USE_DEMO_TRAY") === "1"
    property color ink: "#e9eff4"
    property color muted: "#9baebb"
    property color accent: state.mode === "yolo" ? "#ed8a71" : "#65d7b2"
    function send(op, fields) {
        if(op!=="state") root.lastError="";
        if ((op==="approve" || op==="begin_share") && !durationPicker.valid) {
            root.lastError="Enter a grant duration from 1 to 60 minutes.";
            return;
        }
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
                    // Variants must be keyed by stable window IDs, not the
                    // changing marker objects (whose countdown ticks each second).
                    let ids=(s.targets || []).map(t => t.window.id).sort();
                    if(JSON.stringify(ids)!==JSON.stringify(root.targetIDs)) root.targetIDs=ids;
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
        opacity: enabled ? 1 : 0.45
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
        model: root.targetIDs
        PanelWindow {
            required property string modelData
            property var target: (root.state.targets || []).find(t => t.window.id===modelData)
                || {window:{at:[0,0],size:[0,0]},label:"",remaining_seconds:0}
            screen: Quickshell.screens.find(s => target.window.at[0]>=s.x && target.window.at[0]<s.x+s.width && target.window.at[1]>=s.y && target.window.at[1]<s.y+s.height) || Quickshell.screens[0]
            anchors { top:true; left:true }
            margins { left:Math.max(0,target.window.at[0]-screen.x);top:Math.max(0,target.window.at[1]-screen.y) }
            implicitWidth:target.window.size[0];implicitHeight:target.window.size[1]
            exclusionMode:ExclusionMode.Ignore;color:"transparent";mask:Region {}
            WlrLayershell.namespace:"computer-use-target"
            WlrLayershell.layer:WlrLayer.Overlay
            Rectangle { anchors.fill:parent;color:"transparent";border.width:3;border.color:root.accent;radius:3 }
            Rectangle { anchors.top:parent.top;anchors.horizontalCenter:parent.horizontalCenter;width:markerText.implicitWidth+20;height:23;color:root.accent;radius:4
                Text { id:markerText;anchors.centerIn:parent;text:target.label+(target.input_mode?" · "+target.input_mode:"")+(target.remaining_seconds?" · "+target.remaining_seconds+"s":"");color:"#10251e";font.pixelSize:10;font.bold:true;textFormat:Text.PlainText }
            }
        }
    }
    TrayPlacement { id: trayPlacement }
    PanelWindow {
        id: panel
        visible: root.openPanel && !root.state.picker
        screen: trayPlacement.screenFor(Quickshell.screens, root.state.tray_anchor)
        property var placement: trayPlacement.geometry(screen, root.state.tray_anchor, 440, 770, root.demoTray)
        anchors { top:true; left:true }
        margins { top:panel.placement.y; left:panel.placement.x }
        implicitWidth: placement.width; implicitHeight: placement.height
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
                RowLayout { Layout.fillWidth:true; spacing:16
                    Text { text:"Mode";color:root.ink;font.pixelSize:13 }
                    Rectangle {
                        Layout.fillWidth:true;implicitHeight:44;radius:10;color:"#0d1922";border.color:"#30424e"
                        RowLayout { anchors.fill:parent;anchors.margins:4;spacing:4
                            ActionButton { Layout.fillWidth:true;label:"Approve";tint:root.state.mode==="approve"?"#255749":"#0d1922";onClicked:{root.confirmYolo=false;root.send("mode",{mode:"approve"})} }
                            ActionButton { Layout.fillWidth:true;label:"YOLO";tint:root.state.mode==="yolo"?"#763e32":"#0d1922";onClicked:root.confirmYolo=true }
                        }
                    }
                }
                Rectangle { visible:root.confirmYolo;Layout.fillWidth:true;implicitHeight:88;color:"#4c302a";radius:9
                    ColumnLayout { anchors.fill:parent;anchors.margins:10;spacing:6
                        Text { text:"YOLO allows all exposed tools without prompts.";color:"#ffd6c5";font.pixelSize:12;textFormat:Text.PlainText }
                        ActionButton { label:"Enable YOLO on this desktop";tint:"#95503c";onClicked:{root.send("mode",{mode:"yolo"});root.confirmYolo=false} }
                    }
                }
                Text { Layout.fillWidth:true;wrapMode:Text.WordWrap;text:root.state.mode==="yolo"?"YOLO is active. New tool requests are automatically allowed. Pause still stops everything.":"The MCP client can request access. Only you can grant it. New windows never inherit control.";color:root.muted;font.pixelSize:12 }
                RowLayout { Layout.fillWidth:true;visible:root.state.mode==="approve"
                    Text { text:"Grant duration";color:root.ink;font.pixelSize:13 }
                    Item { Layout.fillWidth:true }
                    DurationPicker {
                        id: durationPicker
                        Layout.fillWidth: false
                        Layout.minimumWidth: 180
                        Layout.preferredWidth: 180
                        Layout.maximumWidth: 180
                    }
                }
                RowLayout { Layout.fillWidth:true;spacing:8
                    ActionButton { label:"Share window";visible:root.state.mode==="approve";enabled:durationPicker.valid;onClicked:root.send("begin_share",{seconds:root.minutes*60,capability:"control"}) }
                    Item { Layout.fillWidth:true }
                    ActionButton { label:root.state.paused?"Resume":"Pause";onClicked:root.send("pause",{paused:!root.state.paused}) }
                }
                Text { visible:root.lastError!=="";Layout.fillWidth:true;wrapMode:Text.WordWrap;text:root.lastError;textFormat:Text.PlainText;color:"#ffae93";font.pixelSize:12 }
                ScrollView { id: scroller; Layout.fillWidth:true;Layout.fillHeight:true;clip:true;contentWidth:availableWidth
                    Column { width:scroller.availableWidth;spacing:12
                        Text { visible:(root.state.oauth_pending||[]).length>0;text:"OAUTH CONNECTION REQUESTS";color:"#89b4fa";font.pixelSize:11;font.bold:true }
                        Repeater { model:root.state.oauth_pending||[]
                            Rectangle { required property var modelData;width:parent.width;implicitHeight:oauthRequest.implicitHeight+24;radius:10;color:"#253044";border.color:"#89b4fa"
                                Column { id:oauthRequest;anchors.left:parent.left;anchors.right:parent.right;anchors.top:parent.top;anchors.margins:12;spacing:8
                                    Text { width:parent.width;text:"Unverified app: "+modelData.name;textFormat:Text.PlainText;wrapMode:Text.WrapAnywhere;color:root.ink;font.pixelSize:14;font.bold:true }
                                    Text { width:parent.width;text:"Redirect: "+modelData.redirect_uri;textFormat:Text.PlainText;wrapMode:Text.WrapAnywhere;color:root.muted;font.pixelSize:11 }
                                    Text { width:parent.width;text:"Allow MCP connection for 1 hour. Window names become visible; pixels, input and recording still need separate grants. Loopback callbacks do not prove the app's identity.";wrapMode:Text.WordWrap;color:root.ink;font.pixelSize:12 }
                                    Row { spacing:8
                                        ActionButton { label:"Allow connection";tint:"#28654f";onClicked:root.send("oauth_approve",{id:modelData.id}) }
                                        ActionButton { label:"Deny";onClicked:root.send("oauth_deny",{id:modelData.id}) }
                                    }
                                }
                            }
                        }
                        Text { text:"REQUESTS  ·  "+root.state.requests.length;color:root.accent;font.pixelSize:11;font.bold:true;font.letterSpacing:1 }
                        Text { visible:root.state.requests.length===0;text:"No requests waiting";color:root.muted;font.pixelSize:13 }
                        Repeater { model:root.state.requests
                            Rectangle { required property var modelData; width:parent.width;implicitHeight:requestColumn.implicitHeight+24;radius:10;color:"#243642";border.color:"#b58c4b"
                                Column { id:requestColumn;anchors.left:parent.left;anchors.right:parent.right;anchors.top:parent.top;anchors.margins:12;spacing:9
                                    Text { width:parent.width;text:modelData.label;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.ink;font.pixelSize:14;font.bold:true }
                                    Text { width:parent.width;text:"Agent reason (untrusted): "+modelData.reason;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.muted;font.pixelSize:12 }
                                    Text { width:parent.width;text:"Client "+modelData.client.slice(0,8);textFormat:Text.PlainText;color:"#839ca9";font.pixelSize:10 }
                                    Row { spacing:8
                                        ActionButton { label:durationPicker.valid?"Grant "+root.minutes+" min":"Enter duration";enabled:durationPicker.valid;tint:"#28654f";onClicked:root.send("approve",{id:modelData.id,seconds:root.minutes*60}) }
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
                        Text { visible:(root.state.oauth_connections||[]).length>0;text:"OAUTH CONNECTIONS";color:"#89b4fa";font.pixelSize:11;font.bold:true }
                        Repeater { model:root.state.oauth_connections||[]
                            Rectangle { required property var modelData;width:parent.width;implicitHeight:oauthConnection.implicitHeight+22;radius:10;color:"#253044"
                                Column { id:oauthConnection;anchors.left:parent.left;anchors.right:parent.right;anchors.top:parent.top;anchors.margins:11;spacing:8
                                    Text { width:parent.width;text:modelData.name+" · expires "+new Date(modelData.expires).toLocaleTimeString();textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:root.ink;font.pixelSize:12 }
                                    ActionButton { label:"Revoke connection";tint:"#693d36";onClicked:root.send("oauth_revoke",{id:modelData.id}) }
                                }
                            }
                        }
                        Text { text:"RECENT ACTIVITY";color:root.muted;font.pixelSize:11;font.bold:true;font.letterSpacing:1 }
                        Repeater { model:root.state.audit.slice(-5).reverse()
                            Text { required property var modelData;width:parent.width;text:modelData.event+" · "+modelData.detail;textFormat:Text.PlainText;wrapMode:Text.WordWrap;color:"#a3b6c0";font.pixelSize:11 }
                        }
                    }
                }
                ActionButton { Layout.fillWidth:true;label:"Revoke desktop grants";tint:"#693d36";onClicked:root.send("revoke_all") }
                Text { Layout.fillWidth:true;wrapMode:Text.WordWrap;text:"Window-scoped input requires the compositor guard. Same-user shell access is outside this permission boundary.";color:"#78909f";font.pixelSize:10 }
            }
        }
    }
}
