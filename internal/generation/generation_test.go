package generation

import (
	"encoding/json"
	"testing"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/timeline"
)

func TestGenerateCreatesBeatEvents(t *testing.T) {
	tl, err := Generate(analysis.SongAnalysis{
		DurationMS: 2000,
		BPM:        120,
		Beats:      []int64{0, 500, 1000},
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.1},
			{TimeMS: 500, Value: 0.9},
		},
	}, Options{Name: "song", Target: "desk"})
	if err != nil {
		t.Fatal(err)
	}

	if tl.Name != "song" {
		t.Fatalf("name = %q, want song", tl.Name)
	}
	if len(tl.Events) <= 1 {
		t.Fatalf("events = %d, want more than startup event", len(tl.Events))
	}
	if tl.Events[0].Action != "power_on" {
		t.Fatalf("first action = %q, want power_on", tl.Events[0].Action)
	}
	if !hasSetColorEvent(tl.Events) {
		t.Fatal("expected at least one set_color event")
	}
}

func TestGenerateRejectsUnknownStyle(t *testing.T) {
	_, err := Generate(analysis.SongAnalysis{
		DurationMS: 1000,
		Beats:      []int64{0},
	}, Options{Style: "unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateStylesAreDistinct(t *testing.T) {
	song := analysis.SongAnalysis{
		DurationMS: 3000,
		BPM:        120,
		Beats:      []int64{0, 500, 1000, 1500, 2000, 2500},
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.2},
			{TimeMS: 1000, Value: 0.7},
		},
	}

	synthwave, err := Generate(song, Options{Style: "synthwave"})
	if err != nil {
		t.Fatal(err)
	}
	minimal, err := Generate(song, Options{Style: "minimal"})
	if err != nil {
		t.Fatal(err)
	}

	synthColor := firstSetColorParams(t, synthwave.Events)
	minimalColor := firstSetColorParams(t, minimal.Events)
	if *synthColor.Hue == *minimalColor.Hue && *synthColor.Saturation == *minimalColor.Saturation {
		t.Fatal("expected styles to produce different palette choices")
	}
}

func TestGenerateUsesSpatialActionsForDiscoveredDevices(t *testing.T) {
	tl, err := Generate(testSong(), Options{
		Target: "all",
		Devices: []devices.DeviceInfo{
			{ID: "desk", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true}},
			{ID: "strip", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, HasColor: true, HasKelvin: true, ZoneCount: 8}},
			{ID: "tile", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMatrix, HasColor: true, HasKelvin: true, MatrixWidth: 4, MatrixHeight: 4}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasAction(tl.Events, "set_color") {
		t.Fatal("expected single-zone set_color event")
	}
	if !hasAction(tl.Events, "set_zone_colors") {
		t.Fatal("expected multizone set_zone_colors event")
	}
	if !hasAction(tl.Events, "set_matrix_colors") {
		t.Fatal("expected matrix set_matrix_colors event")
	}
}

func TestGenerateKeepsCapabilitiesForNamedSelectors(t *testing.T) {
	tl, err := Generate(testSong(), Options{
		Target: "tv,desk",
		Devices: []devices.DeviceInfo{
			{ID: "desk-id", Label: "desk", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true}},
			{ID: "strip-id", Group: "tv", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, HasColor: true, HasKelvin: true, ZoneCount: 8}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasTargetAction(tl.Events, "strip-id", "set_zone_colors") {
		t.Fatal("expected tv group selector to render strip-id with zone colors")
	}
	if !hasTargetAction(tl.Events, "desk-id", "set_color") {
		t.Fatal("expected desk selector to render desk-id with set_color")
	}
}

func TestGenerateKeepsBreakdownResponsive(t *testing.T) {
	tl, err := Generate(analysis.SongAnalysis{
		DurationMS: 30000,
		BPM:        120,
		Beats:      beatsEvery(0, 30000, 500),
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.2},
			{TimeMS: 5000, Value: 0.5},
			{TimeMS: 15000, Value: 0.8},
			{TimeMS: 22000, Value: 0.22},
			{TimeMS: 26000, Value: 0.18},
		},
		Sections: []analysis.Section{
			{StartMS: 0, EndMS: 10000, Type: "drop", Energy: 0.8},
			{StartMS: 10000, EndMS: 25000, Type: "breakdown", Energy: 0.2},
			{StartMS: 25000, EndMS: 30000, Type: "outro", Energy: 0.18},
		},
	}, Options{Name: "song", Target: "desk"})
	if err != nil {
		t.Fatal(err)
	}

	var breakdownEvents []timeline.Event
	for _, event := range tl.Events {
		if event.TimeMS >= 10000 && event.TimeMS < 25000 && event.Action == "set_color" {
			breakdownEvents = append(breakdownEvents, event)
		}
	}
	if len(breakdownEvents) < 10 {
		t.Fatalf("breakdown events = %d, want at least 10", len(breakdownEvents))
	}

	var maxGap int64
	for i := 1; i < len(breakdownEvents); i++ {
		gap := breakdownEvents[i].TimeMS - breakdownEvents[i-1].TimeMS
		if gap > maxGap {
			maxGap = gap
		}
	}
	if maxGap > 2500 {
		t.Fatalf("breakdown max gap = %dms, want <= 2500ms", maxGap)
	}
}

func TestGenerateAddsSectionTransitionAccents(t *testing.T) {
	tl, err := Generate(analysis.SongAnalysis{
		DurationMS: 30000,
		BPM:        120,
		Beats:      beatsEvery(0, 30000, 500),
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.2},
			{TimeMS: 15000, Value: 0.9},
		},
		Sections: []analysis.Section{
			{StartMS: 0, EndMS: 16000, Type: "intro", Energy: 0.2},
			{StartMS: 16000, EndMS: 24000, Type: "build", Energy: 0.55},
			{StartMS: 24000, EndMS: 30000, Type: "drop", Energy: 0.9},
		},
	}, Options{Name: "song", Target: "desk"})
	if err != nil {
		t.Fatal(err)
	}

	if countEventsBetween(tl.Events, 16000, 18000) < 4 {
		t.Fatal("expected build transition accents near 00:16")
	}
	if countEventsBetween(tl.Events, 24000, 27000) < 8 {
		t.Fatal("expected drop transition accents near 00:24")
	}
}

func TestGenerateDoesNotOverlapTransitionsForSameTargetAction(t *testing.T) {
	tl, err := Generate(analysis.SongAnalysis{
		DurationMS: 30000,
		BPM:        120,
		Beats:      beatsEvery(0, 30000, 500),
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.35},
			{TimeMS: 12000, Value: 0.8},
			{TimeMS: 22000, Value: 0.25},
		},
		Sections: []analysis.Section{
			{StartMS: 0, EndMS: 10000, Type: "intro", Energy: 0.3},
			{StartMS: 10000, EndMS: 22000, Type: "drop", Energy: 0.85},
			{StartMS: 22000, EndMS: 30000, Type: "outro", Energy: 0.2},
		},
	}, Options{
		Name:   "song",
		Target: "all",
		Devices: []devices.DeviceInfo{
			{ID: "desk", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true}},
			{ID: "strip", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, HasColor: true, HasKelvin: true, ZoneCount: 8}},
			{ID: "tile", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMatrix, HasColor: true, HasKelvin: true, MatrixWidth: 4, MatrixHeight: 4}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if hasTransitionOverlap(tl.Events) {
		t.Fatal("generated timeline has overlapping transitions for the same target/action")
	}
}

func TestNormalizeTimelineEventsCapsOverlappingTransition(t *testing.T) {
	events := normalizeTimelineEvents([]timeline.Event{
		setColorEvent(1000, "desk", 5000),
		setColorEvent(2000, "desk", 1000),
	})

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	params := setColorParams(t, events[0])
	if params.DurationMS == nil || *params.DurationMS != 1000 {
		t.Fatalf("first duration = %v, want 1000", params.DurationMS)
	}
}

func TestNormalizeTimelineEventsDropsZeroLengthTransition(t *testing.T) {
	events := normalizeTimelineEvents([]timeline.Event{
		setColorEvent(1000, "desk", 5000),
		setColorEvent(1000, "desk", 1000),
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].TimeMS != 1000 {
		t.Fatalf("event time = %d, want 1000", events[0].TimeMS)
	}
}

func hasSetColorEvent(events []timeline.Event) bool {
	for _, event := range events {
		if event.Action == "set_color" {
			return true
		}
	}
	return false
}

func setColorEvent(timeMS int64, target string, durationMS int64) timeline.Event {
	hue := 240.0
	saturation := 100.0
	brightness := 80.0
	kelvin := 3500
	return timeline.Event{
		TimeMS: timeMS,
		Target: target,
		Action: "set_color",
		Params: timeline.MustParams(timeline.SetColorParams{
			Hue:        &hue,
			Saturation: &saturation,
			Brightness: &brightness,
			Kelvin:     &kelvin,
			DurationMS: &durationMS,
		}),
	}
}

func hasAction(events []timeline.Event, action string) bool {
	for _, event := range events {
		if event.Action == action {
			return true
		}
	}
	return false
}

func hasTargetAction(events []timeline.Event, target string, action string) bool {
	for _, event := range events {
		if event.Target == target && event.Action == action {
			return true
		}
	}
	return false
}

func testSong() analysis.SongAnalysis {
	return analysis.SongAnalysis{
		DurationMS: 6000,
		BPM:        120,
		Beats:      []int64{0, 500, 1000, 1500, 2000, 2500, 3000, 3500, 4000, 4500, 5000, 5500},
		Energy: []analysis.EnergyPoint{
			{TimeMS: 0, Value: 0.2},
			{TimeMS: 1500, Value: 0.65},
			{TimeMS: 3000, Value: 0.95},
			{TimeMS: 4500, Value: 0.35},
		},
	}
}

func beatsEvery(start, end, step int64) []int64 {
	var beats []int64
	for t := start; t < end; t += step {
		beats = append(beats, t)
	}
	return beats
}

func countEventsBetween(events []timeline.Event, start, end int64) int {
	var count int
	for _, event := range events {
		if event.Action != "power_on" && event.TimeMS >= start && event.TimeMS < end {
			count++
		}
	}
	return count
}

func firstSetColorParams(t *testing.T, events []timeline.Event) timeline.SetColorParams {
	t.Helper()
	for _, event := range events {
		if event.Action == "set_color" {
			return setColorParams(t, event)
		}
	}
	t.Fatal("missing set_color event")
	return timeline.SetColorParams{}
}

func setColorParams(t *testing.T, event timeline.Event) timeline.SetColorParams {
	t.Helper()

	var params timeline.SetColorParams
	if err := json.Unmarshal(event.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params
}
