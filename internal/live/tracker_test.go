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

func TestStateTrackerDoesNotLearnSustainedMusicAsNoise(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 100; i++ {
		tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -68,
			LowDB:         -70,
			MidDB:         -69,
			HighDB:        -72,
			OnsetStrength: 0.002,
		})
	}
	baseline := tracker.noiseFloorDB
	var state State
	for i := 100; i < 2500; i++ {
		onset := 0.01
		if i%10 == 0 {
			onset = 0.3
		}
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -36,
			LowDB:         -39,
			MidDB:         -37,
			HighDB:        -42,
			OnsetStrength: onset,
		})
	}
	if !state.Active || state.Energy < 0.5 {
		t.Fatalf("sustained music was gated after two minutes: %+v", state)
	}
	if tracker.noiseFloorDB-baseline > 1 {
		t.Fatalf("music moved noise floor from %.2f to %.2f", baseline, tracker.noiseFloorDB)
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

func TestStateTrackerReportsInputMargin(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	state := tracker.Update(Features{At: time.Second, RMSDB: -30, LowDB: -35, MidDB: -32, HighDB: -40})
	if state.InputDB != -30 {
		t.Fatalf("input level = %.1f, want -30", state.InputDB)
	}
	if state.MarginDB != state.InputDB-state.NoiseFloorDB {
		t.Fatalf("margin = %.1f, want input %.1f - floor %.1f", state.MarginDB, state.InputDB, state.NoiseFloorDB)
	}
}

func TestStateTrackerRequiresSupportedTempoAndKeepsEstablishedClock(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	state := tracker.Update(Features{At: time.Second, RMSDB: -20, TempoBPM: 150, TempoConfidence: 0.3})
	if state.TempoBPM != 0 {
		t.Fatalf("low-confidence tempo = %.1f, want unavailable", state.TempoBPM)
	}

	state = tracker.Update(Features{At: 1100 * time.Millisecond, RMSDB: -20, TempoBPM: 180, TempoConfidence: 1})
	if state.TempoBPM != 90 {
		t.Fatalf("initial double-time tempo stabilized to %.1f, want 90", state.TempoBPM)
	}
	for i := 0; i < 120; i++ {
		state = tracker.Update(Features{At: time.Duration(12+i) * 100 * time.Millisecond, RMSDB: -20, TempoBPM: 60, TempoConfidence: 0.2})
	}
	if state.TempoBPM != 90 {
		t.Fatalf("established tempo changed to %.1f with confidence %.2f", state.TempoBPM, state.TempoConfidence)
	}
	if state.TempoConfidence >= 0.2 {
		t.Fatalf("stale tempo confidence remained %.2f, want diagnostic confidence below 0.2", state.TempoConfidence)
	}
}

func TestStateTrackerRollingBandCeilingAvoidsPinnedHighBand(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 60; i++ {
		tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -68,
			LowDB:         -70,
			MidDB:         -70,
			HighDB:        -70,
			OnsetStrength: 0.002,
		})
	}
	var state State
	for i := 60; i < 160; i++ {
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -20,
			LowDB:         -45,
			MidDB:         -35,
			HighDB:        -20,
			OnsetStrength: 0.2,
		})
	}
	if state.High >= 0.98 || state.High <= 0.5 {
		t.Fatalf("rolling high-band level = %.2f, want useful unsaturated range", state.High)
	}
}

func TestStabilizeTempoKeepsOctaveNearCurrentPulse(t *testing.T) {
	if got := stabilizeTempo(60); got != 120 {
		t.Fatalf("60 BPM candidate stabilized to %.1f, want closer 120 BPM octave", got)
	}
	if got := stabilizeTempo(180); got != 90 {
		t.Fatalf("initial 180 BPM candidate stabilized to %.1f, want 90", got)
	}
}

func TestStateTrackerContinuesBeatClockBetweenReliableObservations(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	state := tracker.Update(Features{
		At:              time.Second,
		RMSDB:           -20,
		LowDB:           -25,
		MidDB:           -22,
		HighDB:          -28,
		OnsetStrength:   0.8,
		TempoBPM:        90,
		TempoConfidence: 0.9,
		Beat:            true,
	})
	if !state.Beat {
		t.Fatal("reliable observed beat did not start the clock")
	}

	beats := 0
	for at := 1100 * time.Millisecond; at <= 2500*time.Millisecond; at += 100 * time.Millisecond {
		state = tracker.Update(Features{
			At:              at,
			RMSDB:           -20,
			LowDB:           -25,
			MidDB:           -22,
			HighDB:          -28,
			TempoBPM:        90,
			TempoConfidence: 0.1,
		})
		if state.Beat {
			beats++
		}
	}
	if beats != 2 {
		t.Fatalf("predicted beats = %d, want 2 while confidence temporarily decays", beats)
	}
}
