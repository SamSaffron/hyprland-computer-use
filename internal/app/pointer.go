package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func pointerModifiers(names []string) (uint32, error) {
	var mods uint32
	if len(names) > 3 {
		return 0, errors.New("at most three modifiers")
	}
	for _, name := range names {
		var bit uint32
		switch strings.ToUpper(name) {
		case "CTRL", "CONTROL":
			bit = 4
		case "SHIFT":
			bit = 1
		case "ALT":
			bit = 8
		default:
			return 0, errors.New("modifiers must be CTRL, SHIFT or ALT; global SUPER shortcuts are unsupported")
		}
		if mods&bit != 0 {
			return 0, errors.New("duplicate modifier")
		}
		mods |= bit
	}
	return mods, nil
}

// Decode with action-specific presence checks. A missing coordinate is not (0,0).
func (a *Action) UnmarshalJSON(data []byte) error {
	type plain Action
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{"type": true}
	required := []string{"type"}
	add := func(names ...string) {
		for _, n := range names {
			allowed[n] = true
		}
	}
	switch v.Type {
	case "focus":
	case "key":
		add("key")
		required = append(required, "key")
	case "text":
		add("text")
		required = append(required, "text")
	case "move", "click", "scroll", "drag":
		add("x", "y", "modifiers")
		if v.Type != "drag" || fields["path"] == nil {
			required = append(required, "x", "y")
		}
		switch v.Type {
		case "click":
			add("button", "click_count")
		case "scroll":
			add("delta", "delta_x", "delta_y", "unit")
		case "drag":
			add("button", "to_x", "to_y", "path", "duration_ms")
			if fields["path"] == nil {
				required = append(required, "to_x", "to_y")
			} else {
				for _, k := range []string{"x", "y", "to_x", "to_y"} {
					if fields[k] != nil {
						return fmt.Errorf("path cannot be combined with %s", k)
					}
				}
			}
		}
	default:
		return errors.New("unsupported action")
	}
	for k, raw := range fields {
		if !allowed[k] {
			return fmt.Errorf("%s is not valid for %s", k, v.Type)
		}
		if string(raw) == "null" {
			return fmt.Errorf("%s cannot be null", k)
		}
	}
	for _, k := range required {
		if fields[k] == nil {
			return fmt.Errorf("%s requires %s", v.Type, k)
		}
	}
	if v.Type == "scroll" {
		legacy := fields["delta"] != nil
		axes := fields["delta_x"] != nil || fields["delta_y"] != nil || fields["unit"] != nil
		if legacy == axes {
			return errors.New("scroll requires either legacy delta or delta_x/delta_y with unit")
		}
		if axes && (fields["unit"] == nil || fields["delta_x"] == nil || fields["delta_y"] == nil) {
			return errors.New("scroll axes require unit, delta_x and delta_y (use zero for the unused axis)")
		}
	}
	if fields["click_count"] != nil && (v.ClickCount < 1 || v.ClickCount > 3) {
		return errors.New("click_count must be 1–3")
	}
	if raw := fields["path"]; raw != nil {
		var points []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &points); err != nil {
			return err
		}
		if len(points) < 2 || len(points) > 64 {
			return errors.New("drag path requires 2–64 points")
		}
		for _, p := range points {
			if len(p) != 2 || p["x"] == nil || p["y"] == nil || string(p["x"]) == "null" || string(p["y"]) == "null" {
				return errors.New("each path point requires x and y only")
			}
		}
	}
	*a = Action(v)
	return nil
}

func (a Action) extendedPointer() bool {
	return len(a.Modifiers) > 0 || a.ClickCount > 1 || len(a.Path) > 0 || a.Unit != "" || a.DeltaX != 0 || a.DeltaY != 0
}
func validateExtendedPointer(a Action, size [2]float64) error {
	if _, err := pointerModifiers(a.Modifiers); err != nil {
		return err
	}
	if a.ClickCount < 0 || a.ClickCount > 3 || (a.ClickCount != 0 && a.Type != "click") {
		return errors.New("click_count must be 1–3 and only applies to click")
	}
	if len(a.Modifiers) > 0 && a.Type != "click" && a.Type != "drag" && a.Type != "move" && a.Type != "scroll" {
		return errors.New("modifiers require a pointer action")
	}
	if len(a.Path) > 0 {
		if a.Type != "drag" || len(a.Path) < 2 || len(a.Path) > 64 {
			return errors.New("drag path requires 2–64 points")
		}
		if a.X != 0 || a.Y != 0 || a.ToX != 0 || a.ToY != 0 {
			return errors.New("path and endpoint coordinates are mutually exclusive")
		}
		for _, p := range a.Path {
			if err := validatePointerSize(Action{Type: "move", X: p.X, Y: p.Y}, size); err != nil {
				return err
			}
		}
	}
	if a.Unit != "" || a.DeltaX != 0 || a.DeltaY != 0 {
		if a.Type != "scroll" || a.Delta != 0 {
			return errors.New("scroll axes cannot be combined with legacy delta")
		}
		limit := 1200.0
		switch a.Unit {
		case "wheel_steps":
			limit = 100
		case "logical_pixels":
		default:
			return errors.New("unit must be wheel_steps or logical_pixels")
		}
		for _, v := range []float64{a.DeltaX, a.DeltaY} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > limit || (a.Unit == "wheel_steps" && math.Trunc(v) != v) {
				return errors.New("invalid scroll axis magnitude (integer wheel steps: +/-100; logical pixels: +/-1200)")
			}
		}
	}
	return nil
}
