package rendering

import (
	"encoding/json"
	"math"
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

func TestRenderMatrixPulseRingTravelsOutwards(t *testing.T) {
	device := devices.DeviceInfo{
		ID: "tile",
		Capabilities: devices.DeviceCapabilities{
			Kind:         devices.DeviceKindMatrix,
			MatrixWidth:  8,
			MatrixHeight: 8,
		},
	}

	litByBeat := make([]map[[2]int]bool, 0, 3)
	for beat := 0; beat < 3; beat++ {
		intent := testIntent(IntentPulse)
		intent.BeatIndex = beat

		var params timeline.SetMatrixColorsParams
		if err := json.Unmarshal(Render(intent, device)[0].Params, &params); err != nil {
			t.Fatal(err)
		}

		brightest := 0.0
		for _, pixel := range params.Pixels {
			brightest = math.Max(brightest, pixel.Color.Brightness)
		}
		lit := make(map[[2]int]bool)
		for _, pixel := range params.Pixels {
			if pixel.Color.Brightness > brightest*0.6 {
				lit[[2]int{pixel.X, pixel.Y}] = true
			}
		}
		if len(lit) == 0 || len(lit) == len(params.Pixels) {
			t.Fatalf("beat %d lit %d of %d pixels; the ring should light some but not all", beat, len(lit), len(params.Pixels))
		}
		litByBeat = append(litByBeat, lit)
	}

	// The ring has to move, otherwise the tile shows fixed concentric stripes.
	for beat := 1; beat < len(litByBeat); beat++ {
		if sameLitPixels(litByBeat[beat-1], litByBeat[beat]) {
			t.Fatalf("beats %d and %d light identical pixels; the ring is not expanding", beat-1, beat)
		}
	}
}

func sameLitPixels(a, b map[[2]int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
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

func TestRenderMultiZoneSweepLightsABandThatMoves(t *testing.T) {
	device := devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMultiZone,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMultiZone,
				Zones:     12,
			},
		},
	}

	first := zoneBrightness(t, device, IntentSweep, 1)
	second := zoneBrightness(t, device, IntentSweep, 2)

	// A sweep is a band, not the single dot the previous implementation lit.
	brightest := 0.0
	for _, level := range first {
		brightest = math.Max(brightest, level)
	}
	var lit int
	for _, level := range first {
		if level > brightest*0.6 {
			lit++
		}
	}
	if lit < 2 {
		t.Fatalf("sweep lit %d zones, want a band of at least 2", lit)
	}
	if lit == len(first) {
		t.Fatal("sweep lit every zone, so there is no band to see")
	}

	if sameLevels(first, second) {
		t.Fatal("sweep did not move between beats")
	}
}

// The bug this guards: multizone pulses fell through to a static gradient, so a
// strip only ever changed brightness and looked frozen.
func TestRenderMultiZonePulseVariesAcrossZonesAndBeats(t *testing.T) {
	device := devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMultiZone,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMultiZone,
				Zones:     12,
			},
		},
	}

	first := zoneBrightness(t, device, IntentPulse, 1)
	second := zoneBrightness(t, device, IntentPulse, 2)

	if uniformLevels(first) {
		t.Fatal("every zone has the same brightness; nothing is moving along the strip")
	}
	if sameLevels(first, second) {
		t.Fatal("zone brightness is identical between beats; the pulse is not travelling")
	}
}

// Calm sections use gradients rather than a travelling head, but they still must
// not be frozen.
func TestRenderMultiZoneGradientDriftsBetweenBeats(t *testing.T) {
	device := devices.DeviceInfo{
		ID: "strip",
		Capabilities: devices.DeviceCapabilities{
			Kind: devices.DeviceKindMultiZone,
			Surface: lifxdevice.Surface{
				LightType: lifxdevice.LightTypeMultiZone,
				Zones:     12,
			},
		},
	}

	first := zoneHues(t, device, IntentGradient, 0)
	second := zoneHues(t, device, IntentGradient, 3)
	if sameLevels(first, second) {
		t.Fatal("gradient hues are identical between beats; the drift is not applied")
	}
}

func zoneBrightness(t *testing.T, device devices.DeviceInfo, kind IntentKind, beatIndex int) []float64 {
	t.Helper()
	return zoneValues(t, device, kind, beatIndex, func(c timeline.ColorValue) float64 { return c.Brightness })
}

func zoneHues(t *testing.T, device devices.DeviceInfo, kind IntentKind, beatIndex int) []float64 {
	t.Helper()
	return zoneValues(t, device, kind, beatIndex, func(c timeline.ColorValue) float64 { return c.Hue })
}

func zoneValues(t *testing.T, device devices.DeviceInfo, kind IntentKind, beatIndex int, pick func(timeline.ColorValue) float64) []float64 {
	t.Helper()
	intent := testIntent(kind)
	intent.BeatIndex = beatIndex

	events := Render(intent, device)
	if len(events) == 0 {
		t.Fatal("no events rendered")
	}
	var params timeline.SetZoneColorsParams
	if err := json.Unmarshal(events[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	values := make([]float64, 0, len(params.Zones))
	for _, zone := range params.Zones {
		values = append(values, pick(zone.Color))
	}
	return values
}

func sameLevels(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.01 {
			return false
		}
	}
	return true
}

func uniformLevels(values []float64) bool {
	for i := range values {
		if math.Abs(values[i]-values[0]) > 0.01 {
			return false
		}
	}
	return true
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
