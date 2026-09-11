package live

import (
	"math"
	"time"
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
		OnsetCooldown:       100 * time.Millisecond,
	}
}

type StateTracker struct {
	config       TrackerConfig
	noiseFloorDB float64
	fluxBaseline float64
	energy       float64
	low          float64
	mid          float64
	high         float64
	lastEnergy   float64
	lastOnset    time.Duration
	tempo        float64
	initialized  bool
}

func NewStateTracker(config TrackerConfig) *StateTracker {
	if config.EnergyRangeDB <= 0 {
		config = DefaultTrackerConfig()
	}
	return &StateTracker{config: config, noiseFloorDB: config.InitialNoiseFloorDB}
}

func (t *StateTracker) Update(features Features) State {
	if !t.initialized {
		t.fluxBaseline = math.Max(features.OnsetStrength, t.config.OnsetMinimum/2)
		t.initialized = true
	}
	onsetThreshold := math.Max(t.config.OnsetMinimum, t.fluxBaseline*t.config.OnsetRatio)
	transient := features.OnsetStrength >= onsetThreshold

	// Quiet observations can lower the floor quickly. Louder observations only
	// raise it very slowly so a clap or musical accent cannot redefine silence.
	noiseRate := t.config.NoiseRise
	if features.RMSDB < t.noiseFloorDB {
		noiseRate = t.config.NoiseFall
	} else if transient {
		noiseRate = 0
	}
	t.noiseFloorDB = lerp(t.noiseFloorDB, features.RMSDB, noiseRate)

	rawEnergy := gatedLevel(features.RMSDB, t.noiseFloorDB, t.config)
	t.energy = smooth(t.energy, rawEnergy, t.config.Attack, t.config.Release)
	t.low = smooth(t.low, gatedLevel(features.LowDB, t.noiseFloorDB, t.config), t.config.Attack, t.config.Release)
	t.mid = smooth(t.mid, gatedLevel(features.MidDB, t.noiseFloorDB, t.config), t.config.Attack, t.config.Release)
	t.high = smooth(t.high, gatedLevel(features.HighDB, t.noiseFloorDB, t.config), t.config.Attack, t.config.Release)

	onset := rawEnergy > 0 && features.OnsetStrength >= onsetThreshold && features.At-t.lastOnset >= t.config.OnsetCooldown
	if onset {
		t.lastOnset = features.At
	}
	fluxRate := 0.04
	if features.OnsetStrength > t.fluxBaseline {
		fluxRate = 0.006
	}
	t.fluxBaseline = lerp(t.fluxBaseline, features.OnsetStrength, fluxRate)

	if features.TempoConfidence > 0.2 && features.TempoBPM >= 40 && features.TempoBPM <= 240 {
		if t.tempo == 0 {
			t.tempo = features.TempoBPM
		} else {
			t.tempo = lerp(t.tempo, features.TempoBPM, 0.12)
		}
	}

	trend := clamp(t.energy-t.lastEnergy, -1, 1)
	t.lastEnergy = t.energy
	return State{
		At:              features.At,
		NoiseFloorDB:    t.noiseFloorDB,
		Energy:          t.energy,
		Low:             t.low,
		Mid:             t.mid,
		High:            t.high,
		Trend:           trend,
		Onset:           onset,
		Beat:            features.Beat && features.TempoConfidence > 0.2,
		TempoBPM:        t.tempo,
		TempoConfidence: clamp(features.TempoConfidence, 0, 1),
		Active:          t.energy >= 0.015,
	}
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
