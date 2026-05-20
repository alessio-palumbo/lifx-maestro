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

func TestColorParamsRejectsUnknownFields(t *testing.T) {
	_, err := colorParams(timeline.MustParams(map[string]interface{}{
		"hue":     240,
		"unknown": true,
	}))
	if err == nil {
		t.Fatal("expected error")
	}
}

func ptr[T any](value T) *T {
	return &value
}
