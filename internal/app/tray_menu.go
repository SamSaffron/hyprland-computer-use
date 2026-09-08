package app

import (
	"fmt"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const menuInterface = "com.canonical.dbusmenu"

// A real DBusMenu is needed by tray hosts that don't call ContextMenu.
// Keep policy controls in Quickshell; this menu only opens the local console.
type trayMenu struct{ item *tray }
type menuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}
type menuProperties struct {
	ID         int32
	Properties map[string]dbus.Variant
}
type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func menuProps(id int32, names []string) map[string]dbus.Variant {
	p := map[string]dbus.Variant{}
	if id == 0 {
		p["children-display"] = dbus.MakeVariant("submenu")
	}
	if id == 1 {
		p["label"] = dbus.MakeVariant("Open permission console")
		p["enabled"] = dbus.MakeVariant(true)
		p["visible"] = dbus.MakeVariant(true)
	}
	if len(names) == 0 {
		return p
	}
	filtered := map[string]dbus.Variant{}
	for _, name := range names {
		if v, ok := p[name]; ok {
			filtered[name] = v
		}
	}
	return filtered
}
func menuIDError(id int32) *dbus.Error {
	return dbus.MakeFailedError(fmt.Errorf("unknown menu item %d", id))
}
func (m *trayMenu) GetLayout(parent, depth int32, names []string) (uint32, menuLayout, *dbus.Error) {
	layout := menuLayout{parent, menuProps(parent, names), []dbus.Variant{}}
	if parent != 0 && parent != 1 {
		return 0, layout, menuIDError(parent)
	}
	if parent == 0 && depth != 0 {
		layout.Children = append(layout.Children, dbus.MakeVariant(menuLayout{1, menuProps(1, names), []dbus.Variant{}}))
	}
	return 1, layout, nil
}
func (m *trayMenu) GetGroupProperties(ids []int32, names []string) ([]menuProperties, *dbus.Error) {
	if len(ids) == 0 {
		ids = []int32{0, 1}
	}
	result := []menuProperties{}
	for _, id := range ids {
		if id == 0 || id == 1 {
			result = append(result, menuProperties{id, menuProps(id, names)})
		}
	}
	return result, nil
}
func (m *trayMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	if v, ok := menuProps(id, nil)[name]; ok {
		return v, nil
	}
	return dbus.MakeVariant(""), dbus.MakeFailedError(fmt.Errorf("unknown menu property %d/%s", id, name))
}
func (m *trayMenu) Event(id int32, event string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if id != 0 && id != 1 {
		return menuIDError(id)
	}
	if id == 1 && event == "clicked" {
		return m.item.Activate(0, 0)
	}
	return nil
}
func (m *trayMenu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	failed := []int32{}
	for _, e := range events {
		if m.Event(e.ID, e.EventID, e.Data, e.Timestamp) != nil {
			failed = append(failed, e.ID)
		}
	}
	return failed, nil
}
func (m *trayMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	if id != 0 && id != 1 {
		return false, menuIDError(id)
	}
	return false, nil
}
func (m *trayMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	failed := []int32{}
	for _, id := range ids {
		if _, err := m.AboutToShow(id); err != nil {
			failed = append(failed, id)
		}
	}
	return []int32{}, failed, nil
}
func exportTrayMenu(c *dbus.Conn, path dbus.ObjectPath, item *tray) error {
	menu := &trayMenu{item}
	if err := c.Export(menu, path, menuInterface); err != nil {
		return err
	}
	if _, err := prop.Export(c, path, map[string]map[string]*prop.Prop{menuInterface: {
		"Version": {Value: uint32(3)}, "TextDirection": {Value: "ltr"}, "Status": {Value: "normal"}, "IconThemePath": {Value: []string{}},
	}}); err != nil {
		return err
	}
	return c.Export(introspect.NewIntrospectable(&introspect.Node{Name: string(path), Interfaces: []introspect.Interface{{Name: menuInterface, Methods: introspect.Methods(menu)}, prop.IntrospectData}}), path, "org.freedesktop.DBus.Introspectable")
}
