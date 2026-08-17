package rendering

import (
	"encoding/json"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/timeline"
)

func TestRenderSingleZoneFallsBackToSetColor(t *testing.T) {
	events := Render(testIntent(IntentSweep), devices.DeviceInfo{
		ID: "desk",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindSingleZone,
		},
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Action != "set_color" {
		t.Fatalf("action = %q, want set_color", events[0].Action)
	}

	var params timeline.SetColorParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if got := *params.Saturation; got < 90 || got > 100 {
		t.Fatalf("saturation = %v, want 0-100 range", got)
	}
	if got := *params.Brightness; got < 60 || got > 80 {
		t.Fatalf("brightness = %v, want 0-100 range", got)
	}
}

func TestRenderMultiZoneCreatesZoneColors(t *testing.T) {
	events := Render(testIntent(IntentSweep), devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind:      devices.DeviceKindMultiZone,
			ZoneCount: 4,
		},
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Action != "set_zone_colors" {
		t.Fatalf("action = %q, want set_zone_colors", events[0].Action)
	}

	var params timeline.SetZoneColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Zones) != 4 {
		t.Fatalf("zones = %d, want 4", len(params.Zones))
	}
	if got := params.Zones[0].Color.Saturation; got <= 1 || got > 100 {
		t.Fatalf("zone saturation = %v, want 0-100 range", got)
	}
	if got := params.Zones[0].Color.Brightness; got <= 1 || got > 100 {
		t.Fatalf("zone brightness = %v, want 0-100 range", got)
	}
}

func TestRenderMatrixCreatesMatrixColors(t *testing.T) {
	events := Render(testIntent(IntentPulse), devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind:         devices.DeviceKindMatrix,
			MatrixWidth:  3,
			MatrixHeight: 2,
			MatrixLength: 2,
		},
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Action != "set_matrix_colors" {
		t.Fatalf("action = %q, want set_matrix_colors", events[0].Action)
	}

	var params timeline.SetMatrixColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Pixels) != 6 {
		t.Fatalf("pixels = %d, want 6", len(params.Pixels))
	}
}

func TestRenderMatrixPulseUsesRadialAccentFrame(t *testing.T) {
	intent := testIntent(IntentPulse)
	events := Render(intent, devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind:         devices.DeviceKindMatrix,
			MatrixWidth:  3,
			MatrixHeight: 3,
		},
	})

	var params timeline.SetMatrixColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	var accentPixel *timeline.MatrixColorParams
	for i := range params.Pixels {
		if params.Pixels[i].X == 0 && params.Pixels[i].Y == 0 {
			accentPixel = &params.Pixels[i]
			break
		}
	}
	if accentPixel == nil {
		t.Fatal("accent pixel not found")
	}
	wantHue := intent.Palette.AccentForBeat(intent.BeatIndex).Hue
	if accentPixel.Color.Hue != wantHue {
		t.Fatalf("accent pixel hue = %.1f, want accent hue %.1f", accentPixel.Color.Hue, wantHue)
	}
}

func TestRenderMultiZoneUsesSurfaceZones(t *testing.T) {
	events := Render(testIntent(IntentGradient), devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMultiZone,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMultiZone,
				Zones:     5,
			},
		},
	})

	var params timeline.SetZoneColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Zones) != 5 {
		t.Fatalf("zones = %d, want 5", len(params.Zones))
	}
	for _, zone := range params.Zones {
		if zone.Color.Brightness <= 0 {
			t.Fatalf("zone %+v has zero brightness, want adapted gradient color", zone)
		}
	}
}

func TestRenderMultiZoneSweepUsesBeatIndexedAccent(t *testing.T) {
	events := Render(testIntent(IntentSweep), devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMultiZone,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMultiZone,
				Zones:     5,
			},
		},
	})

	var params timeline.SetZoneColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Zones) != 5 {
		t.Fatalf("zones = %d, want 5", len(params.Zones))
	}

	accentIndex := testIntent(IntentSweep).BeatIndex % len(params.Zones)
	accent := params.Zones[accentIndex].Color
	for i, zone := range params.Zones {
		if i == accentIndex {
			continue
		}
		if zone.Color.Hue == accent.Hue {
			t.Fatalf("zone %d hue %.1f unexpectedly matches accent hue %.1f", i, zone.Color.Hue, accent.Hue)
		}
	}
}

func TestRenderMatrixUsesAdaptedSurfaceSendDimensions(t *testing.T) {
	events := Render(testIntent(IntentMatrixWave), devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMatrix,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMatrix,
				Width:     6,
				Height:    2,
				Zones:     12,
				Matrix: &lifxdevice.MatrixSurface{Chains: []lifxdevice.MatrixChain{{
					Bounds:    lifxdevice.Rect{Width: 3, Height: 2},
					SendWidth: 3,
				}}},
			},
		},
	})

	var params timeline.SetMatrixColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Width != 3 || params.Height != 2 || len(params.Pixels) != 6 {
		t.Fatalf("matrix params = width %d height %d pixels %d, want 3x2 with 6 pixels", params.Width, params.Height, len(params.Pixels))
	}
}

func TestRenderMatrixWaveUsesBeatShiftedColumns(t *testing.T) {
	intent := testIntent(IntentMatrixWave)
	events := Render(intent, devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind:         devices.DeviceKindMatrix,
			MatrixWidth:  4,
			MatrixHeight: 2,
		},
	})

	var params timeline.SetMatrixColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	stops := intent.Palette.GradientStops(4)
	wantHue := stops[intent.BeatIndex%len(stops)].Hue
	if params.Pixels[0].Color.Hue != wantHue {
		t.Fatalf("first pixel hue = %.1f, want shifted hue %.1f", params.Pixels[0].Color.Hue, wantHue)
	}
}

func TestRenderMatrixGradientUsesEffectGradientFrame(t *testing.T) {
	events := Render(testIntent(IntentGradient), devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMatrix,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMatrix,
				Width:     4,
				Height:    2,
				Zones:     8,
			},
		},
	})

	var params timeline.SetMatrixColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Width != 4 || params.Height != 2 || len(params.Pixels) != 8 {
		t.Fatalf("matrix params = width %d height %d pixels %d, want 4x2 with 8 pixels", params.Width, params.Height, len(params.Pixels))
	}
	for _, pixel := range params.Pixels {
		if pixel.Color.Brightness <= 0 {
			t.Fatalf("pixel %+v has zero brightness, want gradient color", pixel)
		}
	}
}

func testIntent(kind IntentKind) EffectIntent {
	p := palette.All()["synthwave"]
	return EffectIntent{
		Kind:       kind,
		TimeMS:     100,
		Target:     "all",
		Color:      p.Primary(),
		Palette:    p,
		Brightness: 0.7,
		DurationMS: 120,
		BeatIndex:  2,
		Section:    "drop",
		Supported:  SupportedDeviceKinds{SingleZone: true, MultiZone: true, Matrix: true},
	}
}
