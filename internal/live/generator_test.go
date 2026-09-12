package live

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
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
		{At: 200 * time.Millisecond, Active: true, Energy: 0.3, Low: 0.5, Mid: 0.2, High: 0.1},
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
		At:     500 * time.Millisecond,
		Active: true,
		Energy: 0.2,
		Low:    0.3,
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

	first := generator.Generate(State{At: 200 * time.Millisecond, Active: true, Energy: 0.4, Mid: 0.5})
	var latest []timeline.Event
	for at := 400 * time.Millisecond; at <= 1600*time.Millisecond; at += 200 * time.Millisecond {
		latest = generator.Generate(State{At: at, Active: true, Energy: 0.4, Mid: 0.5})
	}
	if len(first) != 1 || len(latest) != 1 {
		t.Fatalf("ambient events = %d then %d, want one spatial event each", len(first), len(latest))
	}
	if bytes.Equal(first[0].Params, latest[0].Params) {
		t.Fatal("ambient multizone frame did not move while audio remained active")
	}
}
