package generation

import (
	"testing"

	"lifx-maestro/internal/analysis"
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
	}, Options{Name: "song", Target: "desk", Mode: ModeDefault})
	if err != nil {
		t.Fatal(err)
	}

	if tl.Name != "song" {
		t.Fatalf("name = %q, want song", tl.Name)
	}
	if len(tl.Events) != 4 {
		t.Fatalf("events = %d, want 4", len(tl.Events))
	}
	if tl.Events[0].Action != "power_on" {
		t.Fatalf("first action = %q, want power_on", tl.Events[0].Action)
	}
	if tl.Events[2].Params["brightness"].(float64) <= tl.Events[1].Params["brightness"].(float64) {
		t.Fatal("expected higher energy beat to have higher brightness")
	}
}

func TestGenerateRejectsUnknownMode(t *testing.T) {
	_, err := Generate(analysis.SongAnalysis{
		DurationMS: 1000,
		Beats:      []int64{0},
	}, Options{Mode: "unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
}
