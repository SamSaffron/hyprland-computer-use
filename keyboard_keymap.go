package main

import (
	"fmt"
	"sort"
	"strings"
)

// A self-contained US-ASCII keymap for exactly the keys accepted by keySpec
// and runeKey. No system XKB includes or runtime libxkbcommon dependency.
// XKB keycodes are Linux evdev codes + 8; real modifier indices are the
// standard Shift, Lock, Control, Mod1..Mod5 used by the compositor guard.
func keyboardKeymap() string {
	keys := map[uint32][2]string{}
	for r := rune(32); r <= 126; r++ {
		code, mods, _ := runeKey(r)
		pair := keys[code]
		pair[mods] = fmt.Sprintf("0x%04x", r)
		keys[code] = pair
	}
	for code, sym := range map[uint32]string{1: "Escape", 14: "BackSpace", 15: "Tab", 28: "Return", 111: "Delete", 105: "Left", 106: "Right", 103: "Up", 108: "Down", 102: "Home", 107: "End", 104: "Prior", 109: "Next", 42: "Shift_L", 54: "Shift_R", 29: "Control_L", 97: "Control_R", 56: "Alt_L", 100: "Alt_R", 58: "Caps_Lock", 69: "Num_Lock", 125: "Super_L", 126: "Super_R"} {
		keys[code] = [2]string{sym, sym}
	}
	keys[15] = [2]string{"Tab", "ISO_Left_Tab"}
	for i, code := range []uint32{59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 87, 88} {
		sym := fmt.Sprintf("F%d", i+1)
		keys[code] = [2]string{sym, sym}
	}
	codes := make([]int, 0, len(keys))
	for c := range keys {
		codes = append(codes, int(c))
	}
	sort.Ints(codes)
	var s strings.Builder
	s.WriteString("xkb_keymap {\nxkb_keycodes \"computer-use\" { minimum=8; maximum=255;\n")
	for _, c := range codes {
		fmt.Fprintf(&s, "<K%03d> = %d;\n", c, c+8)
	}
	s.WriteString(`};
xkb_types "computer-use" {
 type "TWO_LEVEL" { modifiers=Shift; map[None]=Level1; map[Shift]=Level2; };
 type "ALPHABETIC" { modifiers=Shift+Lock; map[None]=Level1; map[Shift]=Level2; map[Lock]=Level2; map[Shift+Lock]=Level1; };
};
xkb_compatibility "computer-use" {};
xkb_symbols "computer-use" {
 name[Group1]="English (US)";
`)
	for _, c := range codes {
		pair := keys[uint32(c)]
		if pair[1] == "" {
			pair[1] = pair[0]
		}
		typ := "TWO_LEVEL"
		if pair[0] >= "0x0061" && pair[0] <= "0x007a" {
			typ = "ALPHABETIC"
		}
		fmt.Fprintf(&s, "key <K%03d> { type=\"%s\", [ %s, %s ] };\n", c, typ, pair[0], pair[1])
	}
	s.WriteString(`modifier_map Shift { <K042>, <K054> };
modifier_map Lock { <K058> };
modifier_map Control { <K029>, <K097> };
modifier_map Mod1 { <K056>, <K100> };
modifier_map Mod2 { <K069> };
modifier_map Mod4 { <K125>, <K126> };
};
};
`)
	return s.String() + "\x00"
}
