package rendering

import (
	"encoding/json"
	"testing"

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
