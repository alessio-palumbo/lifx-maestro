package live

import (
	"math"
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

func TestStateTrackerDoesNotCalibrateAlreadyPlayingMusicAsNoise(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	var state State
	for i := 0; i < 80; i++ {
		level := -42.0
		if i%8 < 4 {
			level = -37
		}
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         level,
			LowDB:         level - 4,
			MidDB:         level - 1,
			HighDB:        level - 7,
			OnsetStrength: 0.01,
		})
	}

	if !state.Active || state.Energy < 0.25 {
		t.Fatalf("already-playing music was absorbed by startup calibration: %+v", state)
	}
	if tracker.noiseFloorDB > -55 {
		t.Fatalf("already-playing music raised startup floor to %.2f", tracker.noiseFloorDB)
	}
}

func TestStateTrackerExplicitStartupOnsetStopsCalibration(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	var state State
	for i := 0; i < 80; i++ {
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         -38,
			LowDB:         -43,
			MidDB:         -39,
			HighDB:        -46,
			OnsetStrength: 0.01,
			Onset:         i == 4,
		})
	}

	if !state.Active {
		t.Fatalf("music following a startup onset was gated: %+v", state)
	}
	if tracker.noiseFloorDB > -55 {
		t.Fatalf("startup onset did not protect floor: %.2f", tracker.noiseFloorDB)
	}
}

func TestStateTrackerStartupCalibrationUsesLowEnvelope(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	var state State
	for i := 0; i < 200; i++ {
		level := -48.0
		if i < 8 {
			level = -46
		}
		state = tracker.Update(Features{
			At:            time.Duration(i) * 50 * time.Millisecond,
			RMSDB:         level,
			LowDB:         level - 2,
			MidDB:         level - 1,
			HighDB:        level - 5,
			OnsetStrength: 0.002,
		})
	}

	if state.Active || state.Energy > MinimumActiveEnergy {
		t.Fatalf("steady ambience was not gated after startup outliers: %+v", state)
	}
	if tracker.noiseFloorDB < -50 {
		t.Fatalf("low-envelope calibration did not learn steady ambience: %.2f", tracker.noiseFloorDB)
	}
}

func TestTrackerConfigForSensitivityAdjustsNoiseMargin(t *testing.T) {
	low, err := TrackerConfigForSensitivity("low")
	if err != nil {
		t.Fatal(err)
	}
	normal, err := TrackerConfigForSensitivity("normal")
	if err != nil {
		t.Fatal(err)
	}
	high, err := TrackerConfigForSensitivity("high")
	if err != nil {
		t.Fatal(err)
	}
	if !(low.GateAboveNoiseDB > normal.GateAboveNoiseDB && normal.GateAboveNoiseDB > high.GateAboveNoiseDB) {
		t.Fatalf("sensitivity margins low=%.1f normal=%.1f high=%.1f", low.GateAboveNoiseDB, normal.GateAboveNoiseDB, high.GateAboveNoiseDB)
	}
	if _, err := TrackerConfigForSensitivity("extreme"); err == nil {
		t.Fatal("unsupported sensitivity was accepted")
	}
}

func TestStateReportsEnergyThresholdAboveLearnedFloor(t *testing.T) {
	config := DefaultTrackerConfig()
	tracker := NewStateTracker(config)
	state := tracker.Update(Features{At: time.Second, RMSDB: -70, LowDB: -70, MidDB: -70, HighDB: -70})
	if got, want := state.EnergyThresholdDB, state.NoiseFloorDB+config.GateAboveNoiseDB; got != want {
		t.Fatalf("energy threshold = %.1f, want %.1f", got, want)
	}
}

func TestStateTrackerLetsClapThroughWithoutMovingNoiseFloor(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 80; i++ {
		tracker.Update(Features{At: time.Duration(i) * 50 * time.Millisecond, RMSDB: -52, LowDB: -55, MidDB: -54, HighDB: -56, OnsetStrength: 0.002})
	}
	before := tracker.noiseFloorDB
	state := tracker.Update(Features{At: 4 * time.Second, RMSDB: -12, LowDB: -24, MidDB: -15, HighDB: -13, OnsetStrength: 0.8, Onset: true})

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
			Onset:         onset >= 0.3,
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

func TestStateTrackerKeepsSpectrallySupportedQuietMusicActive(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 80; i++ {
		tracker.Update(Features{
			At:     time.Duration(i) * 50 * time.Millisecond,
			RMSDB:  -72,
			LowDB:  -72,
			MidDB:  -72,
			HighDB: -72,
		})
	}
	baseline := tracker.noiseFloorDB
	var state State
	for i := 80; i < 140; i++ {
		state = tracker.Update(Features{
			At:     time.Duration(i) * 50 * time.Millisecond,
			RMSDB:  -72,
			LowDB:  -71,
			MidDB:  -54,
			HighDB: -52,
		})
	}

	if state.Energy != 0 {
		t.Fatalf("quiet spectral passage energy = %.3f, want honest zero amplitude energy", state.Energy)
	}
	if !state.Active || !state.Sustained || state.Presence < minimumSpectralPresence {
		t.Fatalf("quiet spectral passage was not kept present: %+v", state)
	}
	if tracker.noiseFloorDB-baseline > 0.1 {
		t.Fatalf("spectral passage moved noise floor from %.2f to %.2f", baseline, tracker.noiseFloorDB)
	}
}

func TestStateTrackerDoesNotOpenForOneSteadyBand(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 80; i++ {
		tracker.Update(Features{At: time.Duration(i) * 50 * time.Millisecond, RMSDB: -72, LowDB: -72, MidDB: -72, HighDB: -72})
	}
	var state State
	for i := 80; i < 140; i++ {
		state = tracker.Update(Features{
			At:     time.Duration(i) * 50 * time.Millisecond,
			RMSDB:  -72,
			LowDB:  -72,
			MidDB:  -72,
			HighDB: -42,
		})
	}
	if state.Active || state.Sustained {
		t.Fatalf("one steady spectral band opened the gate: %+v", state)
	}
}

func TestStateTrackerBridgesBriefQuietPassageAndRecovers(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 80; i++ {
		tracker.Update(Features{At: time.Duration(i) * 50 * time.Millisecond, RMSDB: -72, LowDB: -72, MidDB: -72, HighDB: -72})
	}
	music := func(at time.Duration) State {
		return tracker.Update(Features{At: at, RMSDB: -72, LowDB: -70, MidDB: -53, HighDB: -51})
	}
	var state State
	for i := 80; i < 100; i++ {
		state = music(time.Duration(i) * 50 * time.Millisecond)
	}
	if !state.Sustained {
		t.Fatal("spectral input did not become sustained")
	}
	for i := 100; i < 112; i++ {
		state = tracker.Update(Features{At: time.Duration(i) * 50 * time.Millisecond, RMSDB: -80, LowDB: -80, MidDB: -80, HighDB: -80})
	}
	if !state.Active || !state.Sustained {
		t.Fatalf("brief quiet passage stopped motion: %+v", state)
	}
	state = music(112 * 50 * time.Millisecond)
	if !state.Active || !state.Sustained {
		t.Fatalf("spectral input did not recover immediately: %+v", state)
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

func TestSelectTempoCandidatePrefersSlowerSupportedMainPulse(t *testing.T) {
	candidates := []TempoCandidate{
		{BPM: 120, Confidence: 1, Strength: 0.972},
		{BPM: 80, Confidence: 1, Strength: 0.966},
		{BPM: 60, Confidence: 1, Strength: 0.961},
		{BPM: 96, Confidence: 1, Strength: 0.953},
	}
	bpm, _ := selectTempoCandidate(candidates, 0)
	if bpm != 80 {
		t.Fatalf("initial main pulse = %.1f BPM, want 80", bpm)
	}
}

func TestSelectTempoCandidatePreservesEstablishedPulse(t *testing.T) {
	candidates := []TempoCandidate{
		{BPM: 120, Confidence: 0.9, Strength: 0.8},
		{BPM: 80, Confidence: 0.8, Strength: 0.77},
	}
	bpm, _ := selectTempoCandidate(candidates, 119)
	if bpm != 120 {
		t.Fatalf("continued pulse = %.1f BPM, want 120", bpm)
	}
}

func TestStateTrackerRequiresPersistentAlternativeBeforeTempoSwitch(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	features := Features{
		At:              time.Second,
		RMSDB:           -20,
		TempoCandidates: []TempoCandidate{{BPM: 90, Confidence: 0.9, Strength: 0.8}},
	}
	state := tracker.Update(features)
	if state.TempoBPM != 0 {
		t.Fatalf("tempo locked before acquisition at %.1f", state.TempoBPM)
	}
	for i := 0; i < 13; i++ {
		features.At += 100 * time.Millisecond
		state = tracker.Update(features)
	}
	if state.TempoBPM != 90 {
		t.Fatalf("acquired tempo = %.1f, want 90", state.TempoBPM)
	}

	features.At += 100 * time.Millisecond
	features.TempoCandidates = []TempoCandidate{{BPM: 120, Confidence: 0.95, Strength: 0.9}}
	state = tracker.Update(features)
	if state.TempoBPM != 90 {
		t.Fatalf("one alternative changed tempo to %.1f", state.TempoBPM)
	}
	for i := 0; i < 27; i++ {
		features.At += 100 * time.Millisecond
		state = tracker.Update(features)
	}
	if state.TempoBPM != 120 {
		t.Fatalf("persistent alternative left tempo at %.1f, want 120", state.TempoBPM)
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
		Onset:           true,
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

func TestStateTrackerAcquiresTempoFromConsistentOnsets(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	var state State
	for index := 0; index < 4; index++ {
		state = tracker.Update(Features{
			At:            time.Second + time.Duration(index)*750*time.Millisecond,
			RMSDB:         -20,
			RecentRMSDB:   -20,
			HasRecentRMS:  true,
			LowDB:         -25,
			MidDB:         -22,
			HighDB:        -28,
			OnsetStrength: 0.8,
			Onset:         true,
		})
		if !state.Onset {
			t.Fatalf("tap %d was not emitted as an onset", index+1)
		}
	}
	if math.Abs(state.TempoBPM-80) > 0.1 {
		t.Fatalf("tempo after consistent taps = %.1f, want 80", state.TempoBPM)
	}
	if !state.Beat {
		t.Fatal("tempo-acquiring tap did not anchor the beat clock")
	}
}

func TestStateTrackerSuppressesPredictedBeatWhenRecentInputStops(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	tracker.Update(Features{
		At:              time.Second,
		RMSDB:           -20,
		RecentRMSDB:     -20,
		HasRecentRMS:    true,
		LowDB:           -25,
		MidDB:           -22,
		HighDB:          -28,
		OnsetStrength:   0.8,
		Onset:           true,
		TempoBPM:        120,
		TempoConfidence: 0.9,
	})
	state := tracker.Update(Features{
		At:           1500 * time.Millisecond,
		RMSDB:        -20,
		RecentRMSDB:  -90,
		HasRecentRMS: true,
		LowDB:        -25,
		MidDB:        -22,
		HighDB:       -28,
	})
	if state.Beat {
		t.Fatal("rolling-window energy produced a predicted beat after recent input stopped")
	}
	if state.TempoBPM == 0 {
		t.Fatal("silence discarded the learned tempo instead of only suspending its clock")
	}
}

func TestStateTrackerUsesExplicitOnsetOnlyOnceAcrossOverlappingWindows(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	onsets := 0
	for i := 0; i < 8; i++ {
		state := tracker.Update(Features{
			At:            time.Second + time.Duration(i)*100*time.Millisecond,
			RMSDB:         -20,
			LowDB:         -25,
			MidDB:         -22,
			HighDB:        -28,
			OnsetStrength: 0.8,
			Onset:         i == 0,
		})
		if state.Onset {
			onsets++
		}
	}
	if onsets != 1 {
		t.Fatalf("onsets = %d, want one explicit event", onsets)
	}
}

func TestStateTrackerRequiresContinuousInputBeforeSustained(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 6; i++ {
		state := tracker.Update(Features{
			At:     time.Second + time.Duration(i)*100*time.Millisecond,
			RMSDB:  -20,
			LowDB:  -25,
			MidDB:  -22,
			HighDB: -28,
			Onset:  i == 0,
		})
		if state.Sustained {
			t.Fatalf("isolated 500 ms tail became sustained at %s", state.At)
		}
	}

	var state State
	for i := 0; i < 9; i++ {
		state = tracker.Update(Features{
			At:     2*time.Second + time.Duration(i)*100*time.Millisecond,
			RMSDB:  -20,
			LowDB:  -25,
			MidDB:  -22,
			HighDB: -28,
		})
	}
	if !state.Sustained {
		t.Fatal("continuous input did not become sustained")
	}
}

func TestStateTrackerStopsPredictedClockAfterInputEnds(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	for i := 0; i < 10; i++ {
		tracker.Update(Features{
			At:              time.Second + time.Duration(i)*100*time.Millisecond,
			RMSDB:           -20,
			LowDB:           -25,
			MidDB:           -22,
			HighDB:          -28,
			Onset:           i == 0,
			TempoBPM:        120,
			TempoConfidence: 0.9,
		})
	}

	beatsAfterSilence := 0
	for i := 0; i < 35; i++ {
		state := tracker.Update(Features{
			At:     2*time.Second + time.Duration(i)*100*time.Millisecond,
			RMSDB:  -90,
			LowDB:  -90,
			MidDB:  -90,
			HighDB: -90,
		})
		if state.Beat {
			beatsAfterSilence++
		}
	}
	if beatsAfterSilence != 0 {
		t.Fatalf("predicted beats after silence = %d, want none", beatsAfterSilence)
	}
}

func TestStateTrackerAutomaticDynamicsUsesHysteresis(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	if tracker.dynamics != DynamicsCalm {
		t.Fatalf("initial dynamics = %q, want calm", tracker.dynamics)
	}

	var dynamics DynamicsLevel
	for i := 0; i < 60; i++ {
		tracker.energy = 1
		tracker.high = 1
		tracker.tempoConfidence = 1
		_, _, dynamics, _, _ = tracker.updateInterpretation(time.Duration(i)*100*time.Millisecond, i%3 == 0, true)
	}
	if dynamics != DynamicsEnergetic {
		t.Fatalf("sustained high intensity dynamics = %q, want energetic", dynamics)
	}

	for i := 60; i < 75; i++ {
		tracker.energy = 0
		tracker.high = 0
		tracker.tempoConfidence = 0
		_, _, dynamics, _, _ = tracker.updateInterpretation(time.Duration(i)*100*time.Millisecond, false, false)
	}
	if dynamics == DynamicsCalm {
		t.Fatal("dynamics fell to calm without the slower release hysteresis")
	}
	for i := 75; i < 180; i++ {
		tracker.energy = 0
		tracker.high = 0
		_, _, dynamics, _, _ = tracker.updateInterpretation(time.Duration(i)*100*time.Millisecond, false, false)
	}
	if dynamics != DynamicsCalm {
		t.Fatalf("quiet dynamics = %q, want calm", dynamics)
	}
}

func TestStateTrackerSectionNoveltyRequiresRearmAndCooldown(t *testing.T) {
	tracker := NewStateTracker(DefaultTrackerConfig())
	tracker.novelty = 0.5
	_, _, _, _, changed := tracker.updateInterpretation(6*time.Second, false, true)
	if !changed {
		t.Fatal("sustained novelty did not mark a section change")
	}
	tracker.novelty = 0.5
	_, _, _, _, changed = tracker.updateInterpretation(7*time.Second, false, true)
	if changed {
		t.Fatal("novelty retriggered without rearming")
	}
	tracker.novelty = 0.1
	tracker.updateInterpretation(8*time.Second, false, true)
	tracker.novelty = 0.5
	_, _, _, _, changed = tracker.updateInterpretation(13*time.Second, false, true)
	if !changed {
		t.Fatal("rearmed novelty did not trigger after cooldown")
	}
}
