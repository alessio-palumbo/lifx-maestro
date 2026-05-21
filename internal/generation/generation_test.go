package generation

import (
	"encoding/json"
	"testing"

	"lifx-maestro/internal/analysis"
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

func hasSetColorEvent(events []timeline.Event) bool {
	for _, event := range events {
		if event.Action == "set_color" {
			return true
		}
	}
	return false
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
