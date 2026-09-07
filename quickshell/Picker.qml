import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland

PanelWindow {
    id: picker
    required property var brokerState
    property string recipient: ""
    property string selectionID: ""
    property var recipientOptions: []
    property string errorText: ""
    signal selectWindow(string id, string client, string window)
    signal cancelSelection(string id)
    readonly property var selection: brokerState.picker || null
    visible: selection !== null
    screen: Quickshell.screens[0]
    anchors { top:true; bottom:true; left:true; right:true }
    exclusionMode: ExclusionMode.Ignore
    focusable: true
    color: "transparent"
    WlrLayershell.namespace: "computer-use-window-picker"
    WlrLayershell.layer: WlrLayer.Overlay
    onSelectionChanged: {
        if (selection && selection.id !== selectionID) {
            selectionID=selection.id; recipient=selection.client;
            recipientOptions=[{id:"",label:"Choose an MCP connection"}].concat(brokerState.clients||[]);
            keys.forceActiveFocus();
        }
    }
    Item {
        id: keys; anchors.fill:parent; focus:true
        Keys.onEscapePressed: picker.cancelSelection(picker.selectionID)
        Rectangle { anchors.fill:parent; color:"#99101318" }
        MouseArea { anchors.fill:parent; cursorShape:Qt.CrossCursor }
        Repeater {
            model: picker.selection ? picker.selection.windows : []
            Rectangle {
                required property var modelData
                x:modelData.at[0]-picker.screen.x; y:modelData.at[1]-picker.screen.y
                width:modelData.size[0]; height:modelData.size[1]
                color: hit.containsMouse ? "#2265d7b2" : "#08101318"
                border.width:hit.containsMouse?4:1
                border.color:hit.containsMouse?"#65d7b2":"#75868f"
                Rectangle {
                    x:10;y:Math.max(155-parent.y,10);width:Math.min(parent.width-20,label.implicitWidth+24);height:42
                    color:hit.containsMouse?"#28654f":"#17262e";radius:5
                    Text { id:label;anchors.fill:parent;anchors.margins:12;text:parent.parent.modelData.title+" · "+parent.parent.modelData.class;color:"white";elide:Text.ElideRight;textFormat:Text.PlainText;font.pixelSize:14 }
                }
                MouseArea {
                    id:hit;anchors.fill:parent;hoverEnabled:true;cursorShape:Qt.CrossCursor
                    onClicked: {
                        if(picker.recipient) picker.selectWindow(picker.selectionID,picker.recipient,parent.modelData.id);
                    }
                }
            }
        }
        Rectangle {
            anchors.top:parent.top;anchors.horizontalCenter:parent.horizontalCenter;anchors.topMargin:12
            width:Math.min(parent.width-24,1020);height:128;radius:12;color:"#111e28";border.color:"#65d7b2";border.width:2
            ColumnLayout {
                anchors.fill:parent;anchors.margins:14;spacing:7
                RowLayout {
                    Text { text:picker.selection && picker.selection.capability==="observe"?"Click a window to share its pixels":"Click a window to share viewing + control";font.pixelSize:21;font.bold:true;color:"#75e0c1" }
                    Item { Layout.fillWidth:true }
                    Button { text:"Cancel · Esc";onClicked:picker.cancelSelection(picker.selectionID) }
                }
                RowLayout {
                    Text { text:"Recipient";color:"white" }
                    ComboBox {
                        Layout.preferredWidth:280
                        model:picker.recipientOptions
                        textRole:"label";valueRole:"id"
                        currentIndex:Math.max(0,model.findIndex(c=>c.id===picker.recipient))
                        enabled:!picker.selection || picker.selection.client===""
                        onActivated:picker.recipient=currentValue
                    }
                    Text { text:picker.selection?Math.ceil(picker.selection.seconds/60)+" min · no recording · Escape cancels":"";color:"#b5c3cc" }
                }
                Text { Layout.fillWidth:true;elide:Text.ElideRight;text:picker.errorText || "Sharing a terminal grants shell authority. Click the labelled window; no access until you select it.";textFormat:Text.PlainText;color:picker.errorText?"#ffb199":"#b5c3cc";font.pixelSize:12 }
            }
        }
    }
}
