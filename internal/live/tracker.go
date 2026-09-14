package live

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type Sensitivity string

const (
	SensitivityLow    Sensitivity = "low"
	SensitivityNormal Sensitivity = "normal"
	SensitivityHigh   Sensitivity = "high"
)

type TrackerConfig struct {
	InitialNoiseFloorDB float64
	GateAboveNoiseDB    float64
	EnergyRangeDB       float64
	Attack              float64
	Release             float64
	NoiseRise           float64
	NoiseFall           float64
	OnsetRatio          float64
	OnsetMinimum        float64
	OnsetCooldown       time.Duration
}

func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		InitialNoiseFloorDB: -60,
		GateAboveNoiseDB:    6,
		EnergyRangeDB:       30,
		Attack:              0.45,
		Release:             0.12,
		NoiseRise:           0.01,
		NoiseFall:           0.08,
		OnsetRatio:          2.4,
		OnsetMinimum:        0.025,
		OnsetCooldown:       160 * time.Millisecond,
	}
}

func TrackerConfigForSensitivity(value string) (TrackerConfig, error) {
	config := DefaultTrackerConfig()
	switch Sensitivity(strings.ToLower(strings.TrimSpace(value))) {
	case SensitivityLow:
		config.GateAboveNoiseDB = 12
	case "", SensitivityNormal:
		// Default six-decibel margin.
	case SensitivityHigh:
		config.GateAboveNoiseDB = 3
	default:
		return TrackerConfig{}, fmt.Errorf("unsupported live sensitivity %q (use low, normal, or high)", value)
	}
	return config, nil
}

type StateTracker struct {
	config          TrackerConfig
	noiseFloorDB    float64
	bandFloorDB     [3]float64
	bandCeilingDB   [3]float64
	fluxBaseline    float64
	energy          float64
	low             float64
	mid             float64
	high            float64
	lastEnergy      float64
	lastOnset       time.Duration
	tempo           float64
	tempoConfidence float64
	nextBeatAt      time.Duration
	clockHoldUntil  time.Duration
	activeSince     time.Duration
	sustainedUntil  time.Duration
	rawActive       bool
	initialized     bool
	startedAt       time.Duration
	signalSeen      bool
}

const (
	noiseCalibrationDuration  = 3 * time.Second
	noiseCalibrationRate      = 0.08
	noiseTrackingCeiling      = 0.05
	MinimumActiveEnergy       = 0.015
	minimumTempoConfidence    = 0.45
	tempoConfidenceDecay      = 0.985
	bandCeilingHeadroomDB     = 3.0
	bandCeilingMinimumRangeDB = 12.0
	bandCeilingAttack         = 0.35
	bandCeilingRelease        = 0.002
	sustainedInputMinimum     = 0.03
	sustainedAttackDuration   = 650 * time.Millisecond
	sustainedReleaseDuration  = 800 * time.Millisecond
	beatClockHoldDuration     = 1500 * time.Millisecond
)

func NewStateTracker(config TrackerConfig) *StateTracker {
	if config.EnergyRangeDB <= 0 {
		config = DefaultTrackerConfig()
	}
	ceiling := config.InitialNoiseFloorDB + config.GateAboveNoiseDB + config.EnergyRangeDB
	return &StateTracker{
		config:        config,
		noiseFloorDB:  config.InitialNoiseFloorDB,
		bandFloorDB:   [3]float64{config.InitialNoiseFloorDB, config.InitialNoiseFloorDB, config.InitialNoiseFloorDB},
		bandCeilingDB: [3]float64{ceiling, ceiling, ceiling},
	}
}

func (t *StateTracker) Update(features Features) State {
	if !t.initialized {
		t.fluxBaseline = math.Max(features.OnsetStrength, t.config.OnsetMinimum/2)
		t.startedAt = features.At
		t.initialized = true
	}
	onsetThreshold := math.Max(t.config.OnsetMinimum, t.fluxBaseline*t.config.OnsetRatio)
	transient := features.OnsetStrength >= onsetThreshold
	if transient {
		t.signalSeen = true
	}

	// Quiet observations can lower the floor quickly. Louder observations only
	// raise while the input is close to the known baseline. Otherwise sustained
	// music would eventually be learned as ambient noise and close its own gate.
	calibrating := !t.signalSeen && features.At-t.startedAt < noiseCalibrationDuration
	rawEnergy := gatedLevel(features.RMSDB, t.noiseFloorDB, t.config)
	rawLow := gatedLevel(features.LowDB, t.bandFloorDB[0], t.config)
	rawMid := gatedLevel(features.MidDB, t.bandFloorDB[1], t.config)
	rawHigh := gatedLevel(features.HighDB, t.bandFloorDB[2], t.config)
	t.noiseFloorDB = t.updateFloor(t.noiseFloorDB, features.RMSDB, rawEnergy, transient, calibrating)
	t.bandFloorDB[0] = t.updateFloor(t.bandFloorDB[0], features.LowDB, rawLow, transient, calibrating)
	t.bandFloorDB[1] = t.updateFloor(t.bandFloorDB[1], features.MidDB, rawMid, transient, calibrating)
	t.bandFloorDB[2] = t.updateFloor(t.bandFloorDB[2], features.HighDB, rawHigh, transient, calibrating)
	t.bandCeilingDB[0] = updateBandCeiling(t.bandCeilingDB[0], features.LowDB, t.bandFloorDB[0], t.config)
	t.bandCeilingDB[1] = updateBandCeiling(t.bandCeilingDB[1], features.MidDB, t.bandFloorDB[1], t.config)
	t.bandCeilingDB[2] = updateBandCeiling(t.bandCeilingDB[2], features.HighDB, t.bandFloorDB[2], t.config)

	// Re-evaluate against any baseline adjustment made during quiet calibration.
	rawEnergy = gatedLevel(features.RMSDB, t.noiseFloorDB, t.config)
	t.energy = smooth(t.energy, rawEnergy, t.config.Attack, t.config.Release)
	t.low = smooth(t.low, bandLevel(features.LowDB, t.bandFloorDB[0], t.bandCeilingDB[0], t.config), t.config.Attack, t.config.Release)
	t.mid = smooth(t.mid, bandLevel(features.MidDB, t.bandFloorDB[1], t.bandCeilingDB[1], t.config), t.config.Attack, t.config.Release)
	t.high = smooth(t.high, bandLevel(features.HighDB, t.bandFloorDB[2], t.bandCeilingDB[2], t.config), t.config.Attack, t.config.Release)

	onset := rawEnergy > 0 && features.Onset
	if onset {
		t.lastOnset = features.At
	}
	sustained := t.updateSustained(features.At, rawEnergy)
	fluxRate := 0.04
	if features.OnsetStrength > t.fluxBaseline {
		fluxRate = 0.006
	}
	t.fluxBaseline = lerp(t.fluxBaseline, features.OnsetStrength, fluxRate)

	if features.TempoConfidence >= minimumTempoConfidence && features.TempoBPM >= 40 && features.TempoBPM <= 240 {
		candidate := stabilizeTempo(features.TempoBPM)
		if t.tempo == 0 {
			t.tempo = candidate
		} else {
			t.tempo = lerp(t.tempo, candidate, 0.12)
		}
		if t.tempoConfidence == 0 {
			t.tempoConfidence = features.TempoConfidence
		} else {
			t.tempoConfidence = lerp(t.tempoConfidence, features.TempoConfidence, 0.2)
		}
	} else {
		t.tempoConfidence *= tempoConfidenceDecay
	}
	// Confidence controls whether a new estimate may retune the clock. Once a
	// plausible pulse is established, keep it through ambiguous active sections;
	// an older clock is less disruptive than dropping rhythmic output entirely.
	reportedTempo := t.tempo
	observedBeat := onset || (features.Beat && features.TempoConfidence >= minimumTempoConfidence)
	beat := t.updateBeatClock(features.At, reportedTempo, observedBeat, sustained && rawEnergy >= sustainedInputMinimum)

	trend := clamp(t.energy-t.lastEnergy, -1, 1)
	t.lastEnergy = t.energy
	return State{
		At:              features.At,
		InputDB:         features.RMSDB,
		NoiseFloorDB:    t.noiseFloorDB,
		MarginDB:        features.RMSDB - t.noiseFloorDB,
		Energy:          t.energy,
		Low:             t.low,
		Mid:             t.mid,
		High:            t.high,
		Trend:           trend,
		Onset:           onset,
		Beat:            beat,
		TempoBPM:        reportedTempo,
		TempoConfidence: clamp(t.tempoConfidence, 0, 1),
		Active:          t.energy >= MinimumActiveEnergy,
		Sustained:       sustained,
	}
}

func (t *StateTracker) updateSustained(at time.Duration, rawEnergy float64) bool {
	if rawEnergy >= sustainedInputMinimum {
		if !t.rawActive {
			t.activeSince = at
			t.rawActive = true
		}
		if at-t.activeSince >= sustainedAttackDuration {
			t.sustainedUntil = at + sustainedReleaseDuration
		}
	} else {
		t.rawActive = false
		t.activeSince = 0
	}
	return t.sustainedUntil > 0 && at <= t.sustainedUntil
}

func (t *StateTracker) updateBeatClock(at time.Duration, bpm float64, observed, sustained bool) bool {
	if bpm < 40 || bpm > 240 {
		t.nextBeatAt = 0
		return false
	}
	period := time.Duration(float64(time.Minute) / bpm)
	if sustained {
		t.clockHoldUntil = at + beatClockHoldDuration
	} else if observed {
		hold := beatClockHoldDuration
		if 2*period > hold {
			hold = 2 * period
		}
		t.clockHoldUntil = at + hold
	}
	if t.clockHoldUntil == 0 || at > t.clockHoldUntil {
		t.nextBeatAt = 0
		return observed
	}
	if t.nextBeatAt == 0 {
		anchor := at
		if t.lastOnset > 0 && at-t.lastOnset <= period {
			anchor = t.lastOnset
		}
		t.nextBeatAt = anchor + period
		for t.nextBeatAt <= at {
			t.nextBeatAt += period
		}
		return observed
	}

	// A detected transient close to the expected beat corrects accumulated phase
	// error. Other transients remain onset accents without resetting the clock.
	tolerance := period / 4
	if observed && absDuration(at-t.nextBeatAt) <= tolerance {
		t.nextBeatAt = at + period
		return true
	}
	if at < t.nextBeatAt {
		return false
	}
	for t.nextBeatAt <= at {
		t.nextBeatAt += period
	}
	return true
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func updateBandCeiling(current, observed, floor float64, config TrackerConfig) float64 {
	minimum := floor + config.GateAboveNoiseDB + bandCeilingMinimumRangeDB
	target := math.Max(minimum, observed+bandCeilingHeadroomDB)
	rate := bandCeilingRelease
	if target > current {
		rate = bandCeilingAttack
	}
	return math.Max(minimum, lerp(current, target, rate))
}

func bandLevel(value, floor, ceiling float64, config TrackerConfig) float64 {
	start := floor + config.GateAboveNoiseDB
	if ceiling <= start {
		return 0
	}
	return clamp((value-start)/(ceiling-start), 0, 1)
}

func stabilizeTempo(candidate float64) float64 {
	// Beat tracking is octave-ambiguous. Keep Live's lighting pulse in a useful
	// movement range instead of allowing an early half-time estimate to make the
	// show settle at 55-70 BPM.
	for candidate > 140 {
		candidate /= 2
	}
	for candidate < 70 {
		candidate *= 2
	}
	return candidate
}

func (t *StateTracker) updateFloor(current, observed, gated float64, transient, calibrating bool) float64 {
	rate := t.config.NoiseRise
	if observed < current {
		rate = t.config.NoiseFall
	} else if calibrating {
		rate = noiseCalibrationRate
	} else if transient || gated > noiseTrackingCeiling {
		rate = 0
	}
	return lerp(current, observed, rate)
}

func gatedLevel(valueDB, noiseFloorDB float64, config TrackerConfig) float64 {
	return clamp((valueDB-noiseFloorDB-config.GateAboveNoiseDB)/config.EnergyRangeDB, 0, 1)
}

func smooth(current, next, attack, release float64) float64 {
	rate := release
	if next > current {
		rate = attack
	}
	return lerp(current, next, clamp(rate, 0, 1))
}

func lerp(a, b, amount float64) float64 {
	return a + (b-a)*amount
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
