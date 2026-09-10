package app

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"
)

// Capture regions use logical window coordinates, not pixels in an older image.
type CaptureRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type CaptureOptions struct {
	Region    *CaptureRegion `json:"region,omitempty" jsonschema:"Optional crop in window-local logical coordinates; captures actual toplevel at full logical resolution before cropping. Crops may be magnified up to 4x; never a desktop crop."`
	MaxWidth  int            `json:"max_width,omitempty" jsonschema:"0 defaults to 1280; maximum output width 1–1920"`
	MaxHeight int            `json:"max_height,omitempty" jsonschema:"0 defaults to 1920; maximum output height 1–1920, preserving aspect ratio"`
}

func (o CaptureOptions) validate() error {
	if o.MaxWidth < 0 || o.MaxWidth > 1920 || o.MaxHeight < 0 || o.MaxHeight > 1920 {
		return errors.New("max_width and max_height must be 0–1920")
	}
	if r := o.Region; r != nil {
		if r.X < 0 || r.Y < 0 || r.Width < 1 || r.Height < 1 || r.X > 8192 || r.Y > 8192 || r.Width > 8192 || r.Height > 8192 {
			return errors.New("invalid capture region")
		}
	}
	return nil
}
func (o CaptureOptions) bounds(w Window) (CaptureRegion, error) {
	if err := o.validate(); err != nil {
		return CaptureRegion{}, err
	}
	r := CaptureRegion{Width: w.Size[0], Height: w.Size[1]}
	if o.Region != nil {
		r = *o.Region
	}
	if r.X > w.Size[0]-r.Width || r.Y > w.Size[1]-r.Height {
		return r, errors.New("capture region outside window")
	}
	return r, nil
}
func (o CaptureOptions) limits() (int, int) {
	width, height := o.MaxWidth, o.MaxHeight
	if width == 0 {
		width = 1280
	}
	if height == 0 {
		height = 1920
	}
	return width, height
}

// Cropping precedes any output resize. Bound the full toplevel allocation before
// capture and again before decode. A 4x nearest-neighbour zoom preserves raw
// pixel evidence rather than inventing text through an enhancement model.
func cropImage(data []byte, w Window, o CaptureOptions) ([]byte, CaptureRegion, error) {
	r, err := o.bounds(w)
	if err != nil {
		return nil, r, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
		return nil, r, errors.New("capture dimensions exceed crop budget")
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, r, err
	}
	// grim -T can return buffer pixels rather than logical pixels. Resample
	// the requested logical rectangle directly, including fractional scaling,
	// so output offsets describe the requested region, not a rounded crop box.
	sx, sy := float64(cfg.Width)/float64(w.Size[0]), float64(cfg.Height)/float64(w.Size[1])
	mw, mh := o.limits()
	maxZoom := 1.0
	if o.Region != nil {
		maxZoom = 4
	}
	scale := math.Min(maxZoom*math.Min(sx, sy), math.Min(float64(mw)/float64(r.Width), float64(mh)/float64(r.Height)))
	ow, oh := max(1, int(float64(r.Width)*scale)), max(1, int(float64(r.Height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			px := min(cfg.Width-1, int((float64(r.X)+(float64(x)+.5)*float64(r.Width)/float64(ow))*sx))
			py := min(cfg.Height-1, int((float64(r.Y)+(float64(y)+.5)*float64(r.Height)/float64(oh))*sy))
			dst.Set(x, y, src.At(px, py))
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, r, err
	}
	if out.Len() > 16<<20 {
		return nil, r, errors.New("capture_too_large")
	}
	return out.Bytes(), r, nil
}
