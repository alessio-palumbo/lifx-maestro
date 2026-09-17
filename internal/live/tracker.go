package live

import (
	"fmt"
	"math"
	"slices"
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
	presence        float64
	low             float64
	mid             float64
	high            float64
	lastEnergy      float64
	lastOnset       time.Duration
	tempo           float64
	tempoConfidence float64
	pendingTempo    float64
	pendingSince    time.Duration
	nextBeatAt      time.Duration
	clockHoldUntil  time.Duration
	activeSince     time.Duration
	sustainedUntil  time.Duration
	presenceUntil   time.Duration
	rawActive       bool
	onsetTimes      []time.Duration
	shortEnergy     float64
	longEnergy      float64
	intensity       float64
	novelty         float64
	dynamics        DynamicsLevel
	pendingDynamics DynamicsLevel
	dynamicsSince   time.Duration
	previousBands   [3]float64
	noveltyArmed    bool
	lastSectionAt   time.Duration
	initialized     bool
	startedAt       time.Duration
	signalSeen      bool
	startupLevels   [][4]float64
}

const (
	noiseCalibrationDuration   = 3 * time.Second
	noiseObservationDuration   = 750 * time.Millisecond
	noiseCalibrationRate       = 0.08
	noiseCalibrationPercentile = 0.20
	startupLevelVariationDB    = 3.0
	startupBandVariationDB     = 6.0
	noiseTrackingCeiling       = 0.05
	MinimumActiveEnergy        = 0.015
	minimumSpectralPresence    = 0.08
	rawSpectralPresence        = 0.03
	presenceReleaseDuration    = 900 * time.Millisecond
	minimumTempoConfidence     = 0.45
	tempoConfidenceDecay       = 0.985
	bandCeilingHeadroomDB      = 3.0
	bandCeilingMinimumRangeDB  = 12.0
	bandCeilingAttack          = 0.35
	bandCeilingRelease         = 0.002
	sustainedInputMinimum      = 0.03
	sustainedAttackDuration    = 650 * time.Millisecond
	sustainedReleaseDuration   = 800 * time.Millisecond
	beatClockHoldDuration      = 1500 * time.Millisecond
	tempoSwitchDuration        = 2500 * time.Millisecond
	tempoAcquireDuration       = 1200 * time.Millisecond
	activityWindowDuration     = 4 * time.Second
	sectionChangeCooldown      = 6 * time.Second
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
		dynamics:      DynamicsCalm,
		noveltyArmed:  true,
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
	startupElapsed := features.At - t.startedAt
	if startupElapsed < noiseCalibrationDuration {
		t.observeStartup(features)
	}
	if transient || features.Onset || (startupElapsed >= noiseObservationDuration && t.startupVaries()) {
		t.signalSeen = true
	}

	// Quiet observations can lower the floor quickly. Louder observations only
	// raise while the input is close to the known baseline. Otherwise sustained
	// music would eventually be learned as ambient noise and close its own gate.
	rawEnergy := gatedLevel(features.RMSDB, t.noiseFloorDB, t.config)
	rawLow := gatedLevel(features.LowDB, t.bandFloorDB[0], t.config)
	rawMid := gatedLevel(features.MidDB, t.bandFloorDB[1], t.config)
	rawHigh := gatedLevel(features.HighDB, t.bandFloorDB[2], t.config)
	spectralEvidence := secondStrongest(rawLow, rawMid, rawHigh) >= rawSpectralPresence
	presenceEvidence := rawEnergy >= MinimumActiveEnergy || spectralEvidence || (features.Onset && rawEnergy > 0)
	calibrating := !t.signalSeen && startupElapsed < noiseCalibrationDuration
	withinPresenceRelease := t.presenceUntil > 0 && features.At <= t.presenceUntil
	withinSustainedRelease := t.sustainedUntil > 0 && features.At <= t.sustainedUntil
	protectFloor := !calibrating && (presenceEvidence || withinPresenceRelease || withinSustainedRelease)
	floorLevels := [4]float64{features.RMSDB, features.LowDB, features.MidDB, features.HighDB}
	if calibrating {
		floorLevels = t.startupFloorLevels(startupElapsed)
	}
	t.noiseFloorDB = t.updateFloor(t.noiseFloorDB, floorLevels[0], rawEnergy, protectFloor, calibrating)
	t.bandFloorDB[0] = t.updateFloor(t.bandFloorDB[0], floorLevels[1], rawLow, protectFloor, calibrating)
	t.bandFloorDB[1] = t.updateFloor(t.bandFloorDB[1], floorLevels[2], rawMid, protectFloor, calibrating)
	t.bandFloorDB[2] = t.updateFloor(t.bandFloorDB[2], floorLevels[3], rawHigh, protectFloor, calibrating)
	t.bandCeilingDB[0] = updateBandCeiling(t.bandCeilingDB[0], features.LowDB, t.bandFloorDB[0], t.config)
	t.bandCeilingDB[1] = updateBandCeiling(t.bandCeilingDB[1], features.MidDB, t.bandFloorDB[1], t.config)
	t.bandCeilingDB[2] = updateBandCeiling(t.bandCeilingDB[2], features.HighDB, t.bandFloorDB[2], t.config)

	// Re-evaluate against any baseline adjustment made during quiet calibration.
	rawEnergy = gatedLevel(features.RMSDB, t.noiseFloorDB, t.config)
	rawLow = gatedLevel(features.LowDB, t.bandFloorDB[0], t.config)
	rawMid = gatedLevel(features.MidDB, t.bandFloorDB[1], t.config)
	rawHigh = gatedLevel(features.HighDB, t.bandFloorDB[2], t.config)
	t.energy = smooth(t.energy, rawEnergy, t.config.Attack, t.config.Release)
	t.low = smooth(t.low, bandLevel(features.LowDB, t.bandFloorDB[0], t.bandCeilingDB[0], t.config), t.config.Attack, t.config.Release)
	t.mid = smooth(t.mid, bandLevel(features.MidDB, t.bandFloorDB[1], t.bandCeilingDB[1], t.config), t.config.Attack, t.config.Release)
	t.high = smooth(t.high, bandLevel(features.HighDB, t.bandFloorDB[2], t.bandCeilingDB[2], t.config), t.config.Attack, t.config.Release)

	onset := rawEnergy > 0 && features.Onset
	if onset {
		t.lastOnset = features.At
	}
	spectralPresence := secondStrongest(t.low, t.mid, t.high)
	presenceInput := math.Max(t.energy, spectralPresence)
	t.presence = smooth(t.presence, presenceInput, t.config.Attack, t.config.Release)
	active := t.updatePresence(features.At, onset)
	continuousInput := rawEnergy >= sustainedInputMinimum || secondStrongest(rawLow, rawMid, rawHigh) >= rawSpectralPresence
	sustained := t.updateSustained(features.At, continuousInput)
	fluxRate := 0.04
	if features.OnsetStrength > t.fluxBaseline {
		fluxRate = 0.006
	}
	t.fluxBaseline = lerp(t.fluxBaseline, features.OnsetStrength, fluxRate)

	t.updateTempo(features)
	// Confidence controls whether a new estimate may retune the clock. Once a
	// plausible pulse is established, keep it through ambiguous active sections;
	// an older clock is less disruptive than dropping rhythmic output entirely.
	reportedTempo := t.tempo
	observedBeat := onset || (features.Beat && features.TempoConfidence >= minimumTempoConfidence)
	beat := t.updateBeatClock(features.At, reportedTempo, observedBeat, sustained && continuousInput)
	activity, intensity, dynamics, novelty, sectionChange := t.updateInterpretation(features.At, onset, sustained)

	trend := clamp(t.energy-t.lastEnergy, -1, 1)
	t.lastEnergy = t.energy
	return State{
		At:              features.At,
		InputDB:         features.RMSDB,
		NoiseFloorDB:    t.noiseFloorDB,
		MarginDB:        features.RMSDB - t.noiseFloorDB,
		Energy:          t.energy,
		Presence:        t.presence,
		Low:             t.low,
		Mid:             t.mid,
		High:            t.high,
		Trend:           trend,
		Onset:           onset,
		Beat:            beat,
		TempoBPM:        reportedTempo,
		TempoConfidence: clamp(t.tempoConfidence, 0, 1),
		Active:          active,
		Sustained:       sustained,
		Activity:        activity,
		Intensity:       intensity,
		Dynamics:        dynamics,
		Novelty:         novelty,
		SectionChange:   sectionChange,
	}
}

func (t *StateTracker) observeStartup(features Features) {
	t.startupLevels = append(t.startupLevels, [4]float64{
		features.RMSDB,
		features.LowDB,
		features.MidDB,
		features.HighDB,
	})
}

func (t *StateTracker) startupVaries() bool {
	if len(t.startupLevels) < 5 {
		return false
	}
	for band := range 4 {
		minimum := t.startupLevels[0][band]
		maximum := minimum
		for _, levels := range t.startupLevels[1:] {
			minimum = math.Min(minimum, levels[band])
			maximum = math.Max(maximum, levels[band])
		}
		threshold := startupBandVariationDB
		if band == 0 {
			threshold = startupLevelVariationDB
		}
		if maximum-minimum >= threshold {
			return true
		}
	}
	return false
}

func (t *StateTracker) startupFloorLevels(elapsed time.Duration) [4]float64 {
	current := [4]float64{t.noiseFloorDB, t.bandFloorDB[0], t.bandFloorDB[1], t.bandFloorDB[2]}
	if elapsed < noiseObservationDuration {
		latest := t.startupLevels[len(t.startupLevels)-1]
		for i := range current {
			current[i] = math.Min(current[i], latest[i])
		}
		return current
	}

	for band := range current {
		values := make([]float64, len(t.startupLevels))
		for i, levels := range t.startupLevels {
			values[i] = levels[band]
		}
		slices.Sort(values)
		index := int(math.Floor(float64(len(values)-1) * noiseCalibrationPercentile))
		current[band] = values[index]
	}
	return current
}

func (t *StateTracker) updateInterpretation(at time.Duration, onset, sustained bool) (float64, float64, DynamicsLevel, float64, bool) {
	if onset {
		t.onsetTimes = append(t.onsetTimes, at)
	}
	cutoff := at - activityWindowDuration
	first := 0
	for first < len(t.onsetTimes) && t.onsetTimes[first] < cutoff {
		first++
	}
	if first > 0 {
		t.onsetTimes = append(t.onsetTimes[:0], t.onsetTimes[first:]...)
	}
	activity := clamp(float64(len(t.onsetTimes))/12.0, 0, 1)

	t.shortEnergy = lerp(t.shortEnergy, t.energy, 0.18)
	t.longEnergy = lerp(t.longEnergy, t.energy, 0.02)
	contrast := clamp(math.Abs(t.shortEnergy-t.longEnergy)*2.5, 0, 1)
	bandChange := (math.Abs(t.low-t.previousBands[0]) + math.Abs(t.mid-t.previousBands[1]) + math.Abs(t.high-t.previousBands[2])) / 3
	t.previousBands = [3]float64{t.low, t.mid, t.high}
	rawNovelty := clamp(contrast*0.7+bandChange*1.2, 0, 1)
	t.novelty = smooth(t.novelty, rawNovelty, 0.18, 0.04)

	rise := clamp(t.energy-t.longEnergy, 0, 1)
	rawIntensity := clamp(
		t.energy*0.48+
			activity*0.10+
			t.high*0.08+
			clamp(t.tempoConfidence, 0, 1)*0.08+
			contrast*0.14+
			rise*0.12,
		0, 1,
	)
	t.intensity = smooth(t.intensity, rawIntensity, 0.08, 0.03)

	candidate := DynamicsBalanced
	if t.intensity < 0.38 {
		candidate = DynamicsCalm
	} else if t.intensity >= 0.64 {
		candidate = DynamicsEnergetic
	}
	if candidate == t.dynamics {
		t.pendingDynamics = ""
	} else if candidate != t.pendingDynamics {
		t.pendingDynamics = candidate
		t.dynamicsSince = at
	} else {
		delay := 1500 * time.Millisecond
		if dynamicsRank(candidate) < dynamicsRank(t.dynamics) {
			delay = 3 * time.Second
		}
		if at-t.dynamicsSince >= delay {
			t.dynamics = candidate
			t.pendingDynamics = ""
		}
	}
	if !t.noveltyArmed && t.novelty < 0.2 {
		t.noveltyArmed = true
	}
	sectionChange := sustained && t.noveltyArmed && t.novelty >= 0.42 && at-t.lastSectionAt >= sectionChangeCooldown
	if sectionChange {
		t.noveltyArmed = false
		t.lastSectionAt = at
	}
	return activity, t.intensity, t.dynamics, t.novelty, sectionChange
}

func dynamicsRank(level DynamicsLevel) int {
	switch level {
	case DynamicsEnergetic:
		return 2
	case DynamicsBalanced:
		return 1
	default:
		return 0
	}
}

func (t *StateTracker) updateTempo(features Features) {
	candidate, confidence := selectTempoCandidate(features.TempoCandidates, t.tempo)
	fromAlternatives := candidate > 0
	if candidate == 0 && features.TempoConfidence >= minimumTempoConfidence && features.TempoBPM >= 40 && features.TempoBPM <= 240 {
		candidate = stabilizeTempo(features.TempoBPM)
		confidence = features.TempoConfidence
	}
	if candidate == 0 || confidence < minimumTempoConfidence {
		t.tempoConfidence *= tempoConfidenceDecay
		return
	}
	if t.tempo == 0 {
		if !fromAlternatives {
			t.tempo = candidate
			t.pendingTempo = 0
		} else if t.pendingTempo == 0 || relativeTempoDistance(candidate, t.pendingTempo) > 0.08 {
			t.pendingTempo = candidate
			t.pendingSince = features.At
		} else if features.At-t.pendingSince >= tempoAcquireDuration {
			t.tempo = candidate
			t.pendingTempo = 0
		}
	} else if relativeTempoDistance(candidate, t.tempo) <= 0.12 {
		t.tempo = lerp(t.tempo, candidate, 0.12)
		t.pendingTempo = 0
	} else {
		if t.pendingTempo == 0 || relativeTempoDistance(candidate, t.pendingTempo) > 0.08 {
			t.pendingTempo = candidate
			t.pendingSince = features.At
		} else if features.At-t.pendingSince >= tempoSwitchDuration {
			t.tempo = candidate
			t.pendingTempo = 0
			t.nextBeatAt = 0
		}
	}
	if t.tempoConfidence == 0 {
		t.tempoConfidence = confidence
	} else {
		t.tempoConfidence = lerp(t.tempoConfidence, confidence, 0.2)
	}
}

func selectTempoCandidate(candidates []TempoCandidate, current float64) (float64, float64) {
	normalized := make([]TempoCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.BPM = normalizeCandidateTempo(candidate.BPM)
		normalized = append(normalized, candidate)
	}
	candidates = normalized
	bestStrength := -1.0
	for _, candidate := range candidates {
		if candidate.BPM >= 45 && candidate.BPM <= 200 && candidate.Confidence >= minimumTempoConfidence && candidate.Strength > bestStrength {
			bestStrength = candidate.Strength
		}
	}
	if bestStrength < 0 {
		return 0, 0
	}

	var selected TempoCandidate
	if current > 0 {
		bestDistance := math.MaxFloat64
		for _, candidate := range candidates {
			if candidate.Confidence < minimumTempoConfidence || candidate.Strength < bestStrength-0.05 {
				continue
			}
			distance := relativeTempoDistance(candidate.BPM, current)
			if distance < bestDistance {
				selected, bestDistance = candidate, distance
			}
		}
	} else {
		// Autocorrelation commonly gives equally strong note subdivisions. Start
		// from the slowest well-supported musical pulse, while excluding very slow
		// lags that are more useful as bars than beats.
		for _, candidate := range candidates {
			if candidate.Confidence < minimumTempoConfidence || candidate.Strength < bestStrength-0.035 || candidate.BPM < 70 || candidate.BPM > 125 {
				continue
			}
			if selected.BPM == 0 || candidate.BPM < selected.BPM {
				selected = candidate
			}
		}
	}
	if selected.BPM == 0 {
		for _, candidate := range candidates {
			if candidate.Strength == bestStrength {
				selected = candidate
				break
			}
		}
	}
	return selected.BPM, selected.Confidence
}

func normalizeCandidateTempo(bpm float64) float64 {
	for bpm > 0 && bpm < 70 {
		bpm *= 2
	}
	for bpm > 180 {
		bpm /= 2
	}
	return bpm
}

func relativeTempoDistance(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return math.MaxFloat64
	}
	return math.Abs(math.Log2(a / b))
}

func (t *StateTracker) updatePresence(at time.Duration, onset bool) bool {
	evidence := t.energy >= MinimumActiveEnergy || secondStrongest(t.low, t.mid, t.high) >= minimumSpectralPresence || onset
	if evidence {
		t.presenceUntil = at + presenceReleaseDuration
	}
	return t.presenceUntil > 0 && at <= t.presenceUntil
}

func (t *StateTracker) updateSustained(at time.Duration, continuous bool) bool {
	if continuous {
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

func (t *StateTracker) updateFloor(current, observed, gated float64, protected, calibrating bool) float64 {
	rate := t.config.NoiseRise
	if observed < current {
		rate = t.config.NoiseFall
	} else if calibrating {
		rate = noiseCalibrationRate
	} else if protected || gated > noiseTrackingCeiling {
		rate = 0
	}
	return lerp(current, observed, rate)
}

func secondStrongest(a, b, c float64) float64 {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b = c
	}
	if a > b {
		b = a
	}
	return b
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
