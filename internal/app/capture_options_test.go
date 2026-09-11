package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fullPNG(t *testing.T, b *Broker) {
	t.Helper()
	pngFixture(t, b, "")
	im := image.NewRGBA(image.Rect(0, 0, 600, 800))
	for y := 200; y < 240; y++ {
		for x := 100; x < 120; x++ {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.backend.Dir, "frame.png"), out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCropBeforeResizeAndTransform(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	fullPNG(t, b)
	options := CaptureOptions{Region: &CaptureRegion{X: 100, Y: 200, Width: 20, Height: 40}, MaxWidth: 320, MaxHeight: 320}
	meta, data, err := b.observeWithOptions(context.Background(), "a", "abc", options)
	if err != nil {
		t.Fatal(err)
	}
	if meta["image_size"] != [2]int{80, 160} {
		t.Fatal(meta)
	}
	transform := meta["image_to_window"].(map[string]any)
	if transform["scale_x"] != .25 || transform["scale_y"] != .25 || transform["offset_x"] != 100 || transform["offset_y"] != 200 {
		t.Fatal(transform)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{0, 0}, {79, 159}, {40, 80}} {
		r, g, bl, _ := im.At(p.X, p.Y).RGBA()
		if r != 65535 || g != 0 || bl != 0 {
			t.Fatalf("non-crop pixel %v", p)
		}
	}
	args, _ := os.ReadFile(filepath.Join(b.backend.Dir, "frame.png.args"))
	if !strings.Contains(string(args), "-T\nabc\n-s\n1.0000\n") || strings.Contains(string(args), "-g") {
		t.Fatal(string(args))
	}
}
func TestCropAndFullHeightLimit(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	fullPNG(t, b)
	options := CaptureOptions{Region: &CaptureRegion{Width: 600, Height: 800}, MaxWidth: 100, MaxHeight: 80}
	meta, _, err := b.observeWithOptions(context.Background(), "a", "abc", options)
	if err != nil || meta["image_size"] != [2]int{60, 80} {
		t.Fatal(meta, err)
	}
	_, _, err = b.observeWithOptions(context.Background(), "a", "abc", CaptureOptions{MaxHeight: 80})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(b.backend.Dir, "frame.png.args"))
	if !strings.Contains(string(args), "0.1000") {
		t.Fatal(string(args))
	}
}
func TestCropRefusalsAndNoInputOnInvalidObservation(t *testing.T) {
	for _, o := range []CaptureOptions{
		{MaxWidth: -1}, {MaxHeight: 1921}, {Region: &CaptureRegion{Width: 0, Height: 1}},
		{Region: &CaptureRegion{X: -1, Width: 1, Height: 1}}, {Region: &CaptureRegion{X: 590, Width: 20, Height: 20}},
	} {
		b, calls := inputTransactionFixture(t, "")
		fullPNG(t, b)
		_, data, err := b.observeWithOptions(context.Background(), "a", "abc", o)
		if err == nil || data != nil {
			t.Fatal("released invalid crop", o, err)
		}
		if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); !os.IsNotExist(err) {
			t.Fatal("captured before crop prevalidation")
		}
		_, err = b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Then: "screenshot", Observation: &PostInputObservationOptions{CaptureOptions: o}, Actions: []Action{{Type: "key", Key: "ENTER"}}})
		if err == nil || len(calls) != 0 {
			t.Fatal("invalid observation performed input", o, err)
		}
	}
}
func TestPostActionCrop(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	fullPNG(t, b)
	r, _, err := b.inputTool(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Then: "screenshot", Observation: &PostInputObservationOptions{CaptureOptions: CaptureOptions{Region: &CaptureRegion{X: 100, Y: 200, Width: 20, Height: 40}}}, Actions: []Action{{Type: "key", Key: "ENTER"}}})
	if err != nil {
		t.Fatal(err)
	}
	meta := toolMetadata(t, r)
	if meta["status"] != "completed" || meta["observation"].(map[string]any)["status"] != "ok" || len(r.Content) != 2 {
		t.Fatal(meta)
	}
}

func TestCropHiDPIAndFractionalScale(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			w := Window{Size: [2]int{100, 100}}
			im := image.NewRGBA(image.Rect(0, 0, int(100*scale), int(100*scale)))
			for y := 0; y < im.Bounds().Dy(); y++ {
				for x := 0; x < im.Bounds().Dx(); x++ {
					im.Set(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
				}
			}
			var raw bytes.Buffer
			png.Encode(&raw, im)
			o := CaptureOptions{Region: &CaptureRegion{X: 11, Y: 13, Width: 20, Height: 30}, MaxWidth: 80, MaxHeight: 120}
			data, r, err := cropImage(raw.Bytes(), w, o)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := png.Decode(bytes.NewReader(data))
			if got.Bounds().Dx() != 80 || got.Bounds().Dy() != 120 || r != *o.Region {
				t.Fatal(got.Bounds(), r)
			}
			rr, gg, _, _ := got.At(0, 0).RGBA()
			if int(rr>>8) != int(11.125*scale) || int(gg>>8) != int(13.125*scale) {
				t.Fatal(rr, gg)
			}
		})
	}
}
