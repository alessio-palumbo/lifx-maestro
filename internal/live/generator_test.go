package live

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/rendering"
	"lifx-maestro/internal/timeline"
)

func TestGeneratorIsDeterministicForTimestampedStates(t *testing.T) {
	config := GeneratorConfig{
		Style: "synthwave",
		Devices: []devices.DeviceInfo{{
			ID:           "desk",
			Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true},
		}},
	}
	states := []State{
		{At: 200 * time.Millisecond, Active: true, Sustained: true, Energy: 0.3, Low: 0.5, Mid: 0.2, High: 0.1},
		{At: 400 * time.Millisecond, Active: true, Energy: 0.8, Low: 0.2, Mid: 0.3, High: 0.9, Onset: true, TempoBPM: 120},
	}

	generate := func() []byte {
		generator, err := NewGenerator(config)
		if err != nil {
			t.Fatal(err)
		}
		var result []timeline.Event
		for _, state := range states {
			result = append(result, generator.Generate(state)...)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	if string(generate()) != string(generate()) {
		t.Fatal("same timestamped states produced different events")
	}
}

func TestGeneratorIgnoresGatedAudio(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{Style: "minimal", Devices: []devices.DeviceInfo{{ID: "desk", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}}}})
	if err != nil {
		t.Fatal(err)
	}
	if events := generator.Generate(State{At: time.Second, Energy: 0.8}); len(events) != 0 {
		t.Fatalf("quiet state generated %d events", len(events))
	}
}

func TestGeneratorDoesNotRequireEveryFrequencyBand(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Devices: []devices.DeviceInfo{{ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := generator.Generate(State{
		At:        500 * time.Millisecond,
		Active:    true,
		Sustained: true,
		Energy:    0.2,
		Low:       0.3,
	})
	if len(events) == 0 {
		t.Fatal("an active state with empty mid/high bands produced no events")
	}
}

func TestGeneratorUsesCapabilityAwareRendering(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Style:     "neon",
		Intensity: generation.DynamicsEnergetic,
		Devices: []devices.DeviceInfo{
			{ID: "desk", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}},
			{ID: "strip", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, ZoneCount: 8}},
			{ID: "tile", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMatrix, MatrixWidth: 4, MatrixHeight: 4}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := generator.Generate(State{At: time.Second, Active: true, Energy: 0.8, High: 0.9, Onset: true, TempoBPM: 120})
	actions := map[string]bool{}
	for _, event := range events {
		actions[event.Action] = true
	}
	for _, action := range []string{"set_color", "set_zone_colors", "set_matrix_colors"} {
		if !actions[action] {
			t.Fatalf("missing %s from %v", action, actions)
		}
	}
}

func TestGeneratorAdvancesAmbientMultiZoneFrameWithoutAccents(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Style: "synthwave",
		Devices: []devices.DeviceInfo{{
			ID: "strip",
			Capabilities: devices.DeviceCapabilities{
				Kind:      devices.DeviceKindMultiZone,
				ZoneCount: 16,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	first := generator.Generate(State{At: 250 * time.Millisecond, Active: true, Sustained: true, Dynamics: DynamicsBalanced, Energy: 0.4, Mid: 0.5})
	var latest []timeline.Event
	for at := 500 * time.Millisecond; at <= 1750*time.Millisecond; at += 250 * time.Millisecond {
		latest = generator.Generate(State{At: at, Active: true, Sustained: true, Dynamics: DynamicsBalanced, Energy: 0.4, Mid: 0.5})
	}
	if len(first) != 1 || len(latest) != 1 {
		t.Fatalf("ambient events = %d then %d, want one spatial event each", len(first), len(latest))
	}
	if bytes.Equal(first[0].Params, latest[0].Params) {
		t.Fatal("ambient multizone frame did not move while audio remained active")
	}
}

func TestGeneratorDoesNotEchoAnIsolatedOnsetEnergyTail(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Devices: []devices.DeviceInfo{{ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if events := generator.Generate(State{At: time.Second, Active: true, Onset: true, Energy: 0.7}); len(events) != 1 {
		t.Fatalf("onset events = %d, want one", len(events))
	}
	for at := 1100 * time.Millisecond; at <= 1600*time.Millisecond; at += 100 * time.Millisecond {
		if events := generator.Generate(State{At: at, Active: true, Energy: 0.5}); len(events) != 0 {
			t.Fatalf("energy tail generated %d events at %s", len(events), at)
		}
	}
}

func TestIntensityControlsLiveEventDensity(t *testing.T) {
	countEvents := func(intensity generation.DynamicsOverride) int {
		generator, err := NewGenerator(GeneratorConfig{
			Intensity: intensity,
			Devices: []devices.DeviceInfo{{
				ID:           "lamp",
				Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for at := 100 * time.Millisecond; at <= time.Second; at += 100 * time.Millisecond {
			dynamics := DynamicsBalanced
			if intensity == generation.DynamicsCalm {
				dynamics = DynamicsCalm
			} else if intensity == generation.DynamicsEnergetic {
				dynamics = DynamicsEnergetic
			}
			count += len(generator.Generate(State{
				At:       at,
				Active:   true,
				Energy:   0.4,
				High:     0.7,
				Onset:    true,
				TempoBPM: 120,
				Dynamics: dynamics,
			}))
		}
		return count
	}

	calm := countEvents(generation.DynamicsCalm)
	auto := countEvents(generation.DynamicsAuto)
	energetic := countEvents(generation.DynamicsEnergetic)
	if !(calm < auto && auto < energetic) {
		t.Fatalf("event density calm=%d auto=%d energetic=%d", calm, auto, energetic)
	}
}

func TestAutomaticDynamicsControlsLiveEventDensity(t *testing.T) {
	count := func(level DynamicsLevel) int {
		generator, err := NewGenerator(GeneratorConfig{
			Intensity: generation.DynamicsAuto,
			Devices: []devices.DeviceInfo{{
				ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		events := 0
		for at := 100 * time.Millisecond; at <= 2*time.Second; at += 100 * time.Millisecond {
			events += len(generator.Generate(State{
				At: at, Active: true, Sustained: true, Energy: 0.5,
				Onset: true, TempoBPM: 100, Dynamics: level,
			}))
		}
		return events
	}
	calm, balanced, energetic := count(DynamicsCalm), count(DynamicsBalanced), count(DynamicsEnergetic)
	if !(calm < balanced && balanced < energetic) {
		t.Fatalf("automatic density calm=%d balanced=%d energetic=%d", calm, balanced, energetic)
	}
}

func TestGeneratorKeepsEffectsStableUntilSectionChange(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Intensity: generation.DynamicsAuto,
		Devices: []devices.DeviceInfo{{
			ID: "strip", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, ZoneCount: 16},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := State{At: time.Second, Active: true, Sustained: true, Dynamics: DynamicsBalanced, Energy: 0.5}
	if got := generator.ambientIntent(state); got != rendering.IntentGradient {
		t.Fatalf("initial ambient intent = %q", got)
	}
	state.SectionChange = true
	generator.Generate(state)
	state.SectionChange = false
	if got := generator.ambientIntent(state); got != rendering.IntentSweep {
		t.Fatalf("next phrase ambient intent = %q", got)
	}
}

func TestGeneratorChangesStyleWithoutResettingProgress(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{
		Style: "synthwave",
		Devices: []devices.DeviceInfo{{
			ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := generator.Generate(State{
		At: time.Second, Active: true, Energy: 0.7, Mid: 0.6, Onset: true, TempoBPM: 100,
	})
	beatIndex := generator.beatIndex
	motion := generator.motion

	if err := generator.SetStyle("warm"); err != nil {
		t.Fatal(err)
	}
	if generator.beatIndex != beatIndex || generator.motion != motion {
		t.Fatal("changing style reset generator progress")
	}
	after := generator.Generate(State{
		At: 2 * time.Second, Active: true, Energy: 0.7, Mid: 0.6, Onset: true, TempoBPM: 100,
	})
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("events before=%d after=%d, want one each", len(before), len(after))
	}
	if bytes.Equal(before[0].Params, after[0].Params) {
		t.Fatal("changing style did not change rendered output")
	}
}

func TestGeneratorRejectsUnknownStyleWithoutChangingCurrentStyle(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	before := generator.currentStyle()
	if err := generator.SetStyle("unknown"); err == nil {
		t.Fatal("unknown style was accepted")
	}
	if after := generator.currentStyle(); after.Name != before.Name {
		t.Fatalf("style changed from %q to %q after rejected update", before.Name, after.Name)
	}
}

func TestGeneratorChangesIntensityWithoutResettingProgress(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{Style: "synthwave", Intensity: generation.DynamicsCalm})
	if err != nil {
		t.Fatal(err)
	}
	before := generator.currentStyle()
	generator.beatIndex = 4
	generator.motion = 2.5

	if err := generator.SetIntensity(generation.DynamicsEnergetic); err != nil {
		t.Fatal(err)
	}
	if generator.beatIndex != 4 || generator.motion != 2.5 {
		t.Fatal("changing intensity reset generator progress")
	}
	after := generator.currentStyle()
	if after.BrightnessScale <= before.BrightnessScale {
		t.Fatalf("brightness scale = %.2f after energetic, want greater than calm %.2f", after.BrightnessScale, before.BrightnessScale)
	}
}

func TestGeneratorRejectsInvalidIntensityWithoutChangingCurrentStyle(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{Style: "synthwave", Intensity: generation.DynamicsCalm})
	if err != nil {
		t.Fatal(err)
	}
	before := generator.currentStyle()
	if err := generator.SetIntensity("extreme"); err == nil {
		t.Fatal("invalid intensity was accepted")
	}
	if after := generator.currentStyle(); after.BrightnessScale != before.BrightnessScale {
		t.Fatalf("brightness scale changed from %.2f to %.2f after rejected update", before.BrightnessScale, after.BrightnessScale)
	}
}
