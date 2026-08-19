package effects

import (
	"encoding/json"
	"testing"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/sections"
	"lifx-maestro/internal/timeline"
)

func testContext(sectionType sections.Type, energy float64) Context {
	beats := make([]int64, 0, 16)
	for i := int64(0); i < 16; i++ {
		beats = append(beats, i*500)
	}
	points := make([]analysis.EnergyPoint, 0, 16)
	for i := int64(0); i < 16; i++ {
		points = append(points, analysis.EnergyPoint{TimeMS: i * 500, Value: energy})
	}

	return Context{
		Section:    sections.Section{StartMS: 0, EndMS: 8000, Type: sectionType, Energy: energy},
		Beats:      beats,
		Energy:     points,
		Targets:    []Target{{DeviceID: "desk", Index: 0, Total: 1, Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true, ZoneCount: 1}}},
		Palette:    palette.All()["synthwave"],
		MinBright:  0.2,
		MaxBright:  0.9,
		DurationMS: 200,
		BeatStep:   1,
	}
}

func params(t *testing.T, event timeline.Event) map[string]float64 {
	t.Helper()
	var raw map[string]float64
	if err := json.Unmarshal(event.Params, &raw); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	return raw
}

// A flat energy curve is the case that used to produce an invisible timeline: one
// event per beat, all at the same level. Each beat must now rise to a peak and
// fall back, so it reads as a hit rather than a held level.
func TestPulseGivesEachBeatARiseAndFall(t *testing.T) {
	events := Pulse{}.Generate(testContext(sections.TypeBreakdown, 0.5))
	if len(events) < 4 {
		t.Fatalf("expected several events, got %d", len(events))
	}

	var falls int
	for i := 1; i < len(events); i++ {
		previous, current := params(t, events[i-1]), params(t, events[i])
		if events[i].TimeMS > events[i-1].TimeMS && current["brightness"] < previous["brightness"] {
			falls++
		}
	}
	if falls == 0 {
		t.Fatal("no beat fell back from its peak; the envelope is not being emitted")
	}
}

// With flat energy, brightness alone cannot distinguish beats, so the colour has
// to change. A single-colour Pulse was why quiet sections looked frozen.
func TestPulseAlternatesColours(t *testing.T) {
	events := Pulse{}.Generate(testContext(sections.TypeBreakdown, 0.5))

	hues := make(map[float64]bool)
	for _, event := range events {
		hues[params(t, event)["hue"]] = true
	}
	if len(hues) < 2 {
		t.Fatalf("expected more than one hue across beats, got %d", len(hues))
	}
}

// The downbeat of each bar should be the loudest beat, which is what carries a
// pulse when the track's own energy is constant.
func TestAccentEmphasisesTheDownbeat(t *testing.T) {
	if accentFor(0) <= accentFor(1) {
		t.Fatal("downbeat should be louder than the beat after it")
	}
	if accentFor(2) <= accentFor(3) {
		t.Fatal("third beat of the bar should be louder than the fourth")
	}
}

// The envelope must not outlast the beat, or events on one target overlap and the
// timeline normaliser has to discard them.
func TestEnvelopeFitsWithinTheBeat(t *testing.T) {
	ctx := testContext(sections.TypeDrop, 0.7)
	gapMS := int64(500)

	for beatIndex := 0; beatIndex < 4; beatIndex++ {
		envelope := envelopeFor(0.8, beatIndex, gapMS, ctx)
		if envelope.riseMS <= 0 {
			t.Fatalf("beat %d has a non-positive rise", beatIndex)
		}
		if total := envelope.riseMS + envelope.fallMS; total > gapMS {
			t.Fatalf("beat %d envelope lasts %dms, longer than the %dms beat", beatIndex, total, gapMS)
		}
		if envelope.rest > envelope.peak {
			t.Fatalf("beat %d rests above its peak", beatIndex)
		}
	}
}

// A gap too short to hold a fall should yield a single event rather than one the
// normaliser would drop for being too brief.
func TestEnvelopeSkipsUnusableFall(t *testing.T) {
	ctx := testContext(sections.TypeDrop, 0.7)

	if envelopeFor(0.8, 0, 40, ctx).decays() {
		t.Fatal("a 40ms beat should not emit a separate fall")
	}
	if !envelopeFor(0.8, 0, 500, ctx).decays() {
		t.Fatal("a 500ms beat should emit a fall")
	}
}
