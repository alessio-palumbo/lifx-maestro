package live

import (
	"testing"
	"time"
)

func TestStateTrackerGatesSteadyRoomNoise(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	var state State
	for i := 0; i < 200; i++ {
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -48,
			LowDB:         -50,
			MidDB:         -49,
			HighDB:        -53,
			OnsetStrength: 0.002,
		})
	}
	if state.Active || state.Energy > 0.015 {
		t.Fatalf("steady noise remained active: %+v", state)
	}
}

func TestStateTrackerLetsClapThroughWithoutMovingNoiseFloor(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 80; i++ {
		tracker.Update(Features{At: time.Duration(i) * 50 * time.Millisecond, RMSDB: -52, LowDB: -55, MidDB: -54, HighDB: -56, OnsetStrength: 0.002})
	}
	before := tracker.noiseFloorDB
	state := tracker.Update(Features{At: 4 * time.Second, RMSDB: -12, LowDB: -24, MidDB: -15, HighDB: -13, OnsetStrength: 0.8})

	if !state.Active || !state.Onset {
		t.Fatalf("clap was not detected: %+v", state)
	}
	if state.NoiseFloorDB-before > 0.2 {
		t.Fatalf("clap moved noise floor from %.2f to %.2f", before, state.NoiseFloorDB)
	}
}

func TestStateTrackerSmoothsEnergyRelease(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	tracker.Update(Features{At: time.Second, RMSDB: -10, LowDB: -10, MidDB: -10, HighDB: -10, OnsetStrength: 0.2})
	high := tracker.energy
	state := tracker.Update(Features{At: 1050 * time.Millisecond, RMSDB: -80, LowDB: -80, MidDB: -80, HighDB: -80})
	if state.Energy <= 0 || state.Energy >= high {
		t.Fatalf("release energy = %.3f, previous %.3f", state.Energy, high)
	}
}
