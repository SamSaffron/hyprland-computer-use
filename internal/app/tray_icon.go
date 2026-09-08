package app

import "math"

// StatusNotifierItem pixmaps use network-order ARGB, not Go's RGBA order.
// Draw at several tray sizes with supersampled edges: a quiet monitor outline
// and a state-coloured pointer, with no badge background or tiny lettering.
func icon(r, g, b byte) []iconPixmap {
	var icons []iconPixmap
	for _, size := range []int{16, 22, 32, 48, 64} {
		data := make([]byte, size*size*4)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				var count, red, green, blue int
				for sy := 0; sy < 4; sy++ {
					for sx := 0; sx < 4; sx++ {
						px := (float64(x) + (float64(sx)+0.5)/4) * 32 / float64(size)
						py := (float64(y) + (float64(sy)+0.5)/4) * 32 / float64(size)
						cr, cg, cb, visible := iconSample(px, py, r, g, b)
						if visible {
							count++
							red += int(cr)
							green += int(cg)
							blue += int(cb)
						}
					}
				}
				if count > 0 {
					i := (y*size + x) * 4
					data[i], data[i+1], data[i+2], data[i+3] = byte(count*255/16), byte(red/count), byte(green/count), byte(blue/count)
				}
			}
		}
		icons = append(icons, iconPixmap{int32(size), int32(size), data})
	}
	return icons
}

type iconPoint struct{ x, y float64 }

var pointerOutline = []iconPoint{{18, 13}, {18, 27}, {21.5, 24}, {24, 29}, {27, 27.5}, {24.5, 22.5}, {29, 22}}

func iconSample(x, y float64, r, g, b byte) (byte, byte, byte, bool) {
	inside := false
	border := false
	for i, a := range pointerOutline {
		z := pointerOutline[(i+1)%len(pointerOutline)]
		if (a.y > y) != (z.y > y) && x < (z.x-a.x)*(y-a.y)/(z.y-a.y)+a.x {
			inside = !inside
		}
		if segmentDistance(x, y, a.x, a.y, z.x, z.y) < 1 {
			border = true
		}
	}
	if inside {
		return r, g, b, true
	}
	if border {
		return 17, 30, 40, true
	}
	// Rounded monitor frame, stand and foot.
	qx, qy := math.Abs(x-15)-8, math.Abs(y-13.5)-5.5
	d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - 2
	if math.Abs(d) <= 1 || segmentDistance(x, y, 15, 22, 15, 26) <= 1 || segmentDistance(x, y, 10, 26, 20, 26) <= 1 {
		return 222, 232, 242, true
	}
	return 0, 0, 0, false
}

func segmentDistance(x, y, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-ax-t*dx, y-ay-t*dy)
}
