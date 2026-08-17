package playback

import (
	"testing"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/timeline"
)

func TestColorParamsDecodesTypedParams(t *testing.T) {
	got, err := colorParams(timeline.MustParams(timeline.SetColorParams{
		Hue:        ptr(240.0),
		Saturation: ptr(1.0),
		Brightness: ptr(0.8),
		Kelvin:     ptr(3500),
		DurationMS: ptr(int64(150)),
	}))
	if err != nil {
		t.Fatal(err)
	}

	want := devices.ColorParams{
		Hue:        240,
		Saturation: 1.0,
		Brightness: 0.8,
		Kelvin:     3500,
		DurationMS: 150,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestColorParamsFloorsZeroBrightness(t *testing.T) {
	got, err := colorParams(timeline.MustParams(timeline.SetColorParams{
		Brightness: ptr(0.0),
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got.Brightness != 1 {
		t.Fatalf("brightness = %v, want 1", got.Brightness)
	}
}

func TestColorParamsRejectsUnknownFields(t *testing.T) {
	_, err := colorParams(timeline.MustParams(map[string]interface{}{
		"hue":     240,
		"unknown": true,
	}))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestZoneColorParamsDecodesTypedParams(t *testing.T) {
	got, err := zoneColorParams(timeline.MustParams(timeline.SetZoneColorsParams{
		DurationMS: 90,
		Zones: []timeline.ZoneColorParams{
			{Index: 1, Color: timeline.ColorValue{Hue: 180, Saturation: 0.9, Brightness: 0.7, Kelvin: 4000}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got.durationMS != 90 {
		t.Fatalf("durationMS = %d, want 90", got.durationMS)
	}
	want := devices.ZoneColorParams{Index: 1, Hue: 180, Saturation: 0.9, Brightness: 0.7, Kelvin: 4000}
	if len(got.zones) != 1 || got.zones[0] != want {
		t.Fatalf("zones = %+v, want %+v", got.zones, []devices.ZoneColorParams{want})
	}
}

func TestMatrixColorParamsDecodesTypedParams(t *testing.T) {
	got, err := matrixColorParams(timeline.MustParams(timeline.SetMatrixColorsParams{
		Width:      2,
		Height:     2,
		DurationMS: 75,
		Pixels: []timeline.MatrixColorParams{
			{X: 1, Y: 0, Color: timeline.ColorValue{Hue: 45, Saturation: 0.8, Brightness: 0.6, Kelvin: 3000}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got.width != 2 || got.height != 2 || got.durationMS != 75 {
		t.Fatalf("got width=%d height=%d duration=%d", got.width, got.height, got.durationMS)
	}
	want := devices.MatrixColorParams{X: 1, Y: 0, Hue: 45, Saturation: 0.8, Brightness: 0.6, Kelvin: 3000}
	if len(got.pixels) != 1 || got.pixels[0] != want {
		t.Fatalf("pixels = %+v, want %+v", got.pixels, []devices.MatrixColorParams{want})
	}
}

func ptr[T any](value T) *T {
	return &value
}
