package app

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// MCP schemas describe legal actions rather than a bag of zero-filled fields.
// Runtime decoding and broker/native validation enforce the same restrictions.
func inputWindowSchema() any {
	schema, err := jsonschema.For[InputArgs](nil)
	if err != nil {
		panic(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		panic(err)
	}
	number := func() any { return map[string]any{"type": "number", "minimum": 0} }
	enum := func(values ...string) any { return map[string]any{"type": "string", "enum": values} }
	point := map[string]any{"type": "object", "properties": map[string]any{"x": number(), "y": number()}, "required": []string{"x", "y"}, "additionalProperties": false}
	mods := map[string]any{"type": "array", "items": enum("CTRL", "SHIFT", "ALT"), "maxItems": 3, "uniqueItems": true}
	variant := func(kind string, required []string, props map[string]any) any {
		props["type"] = enum(kind)
		return map[string]any{"type": "object", "properties": props, "required": append([]string{"type"}, required...), "additionalProperties": false}
	}
	pointer := func() map[string]any { return map[string]any{"x": number(), "y": number(), "modifiers": mods} }
	click := pointer()
	click["button"] = enum("left", "right", "middle")
	click["click_count"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "description": "Consecutive clicks in one bounded transaction; defaults to 1."}
	drag := pointer()
	drag["to_x"] = number()
	drag["to_y"] = number()
	drag["button"] = enum("left", "right", "middle")
	drag["duration_ms"] = map[string]any{"type": "integer", "enum": []int{0}, "deprecated": true, "description": "Deprecated compatibility field. Only zero is supported; no timed or held input."}
	path := map[string]any{"path": map[string]any{"type": "array", "items": point, "minItems": 2, "maxItems": 64}, "modifiers": mods, "button": enum("left", "right", "middle"), "duration_ms": drag["duration_ms"]}
	legacyScroll := pointer()
	legacyScroll["delta"] = map[string]any{"type": "number", "minimum": -1200, "maximum": 1200, "description": "Legacy vertical delta, original backend semantics. Prefer explicit axes and unit."}
	axes := func(unit string, integer bool, limit int) map[string]any {
		p := pointer()
		typ := "number"
		if integer {
			typ = "integer"
		}
		for _, axis := range []string{"delta_x", "delta_y"} {
			p[axis] = map[string]any{"type": typ, "minimum": -limit, "maximum": limit, "description": "Positive right/down; both axes required in schema (use 0 for unused axis)."}
		}
		p["unit"] = enum(unit)
		return p
	}
	variants := []any{
		variant("focus", nil, map[string]any{}),
		variant("move", []string{"x", "y"}, pointer()),
		variant("click", []string{"x", "y"}, click),
		variant("drag", []string{"x", "y", "to_x", "to_y"}, drag),
		variant("drag", []string{"path"}, path),
		variant("scroll", []string{"x", "y", "delta"}, legacyScroll),
		variant("scroll", []string{"x", "y", "delta_x", "delta_y", "unit"}, axes("wheel_steps", true, 100)),
		variant("scroll", []string{"x", "y", "delta_x", "delta_y", "unit"}, axes("logical_pixels", false, 1200)),
		variant("key", []string{"key"}, map[string]any{"key": map[string]any{"type": "string", "minLength": 1, "description": "US-layout key chord, e.g. CTRL+L, ENTER. No SUPER/global shortcuts."}}),
		variant("text", []string{"text"}, map[string]any{"text": map[string]any{"type": "string", "description": "UTF-8 keyboard input, not clipboard paste. LF/TAB send Return/Tab; CR and other controls refused. Aggregate batch limit 262144 bytes."}}),
	}
	props := root["properties"].(map[string]any)
	props["actions"] = map[string]any{"type": "array", "items": map[string]any{"anyOf": variants}, "minItems": 1, "maxItems": 128, "description": "Ordered, prevalidated actions in window-local logical coordinates (or selected surface-local)."}
	props["then"] = enum("", "state", "screenshot")
	return root
}
