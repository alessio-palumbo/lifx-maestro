package effects

import (
	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/rendering"
	"lifx-maestro/internal/sections"
	"lifx-maestro/internal/timeline"
)

type ZoneRange struct {
	Start int
	End   int
}

type MatrixRegion struct {
	X      int
	Y      int
	Width  int
	Height int
}

type Target struct {
	DeviceID     string
	Index        int
	Total        int
	Zone         *int
	ZoneRange    *ZoneRange
	MatrixRegion *MatrixRegion
	Capabilities devices.DeviceCapabilities
}

type Context struct {
	Section     sections.Section
	Beats       []int64
	Energy      []analysis.EnergyPoint
	Targets     []Target
	Palette     palette.Palette
	MinBright   float64
	MaxBright   float64
	DurationMS  int64
	BeatStep    int
	TargetShift int
}

type Effect interface {
	Generate(ctx Context) []timeline.Event
}

type Pulse struct{}
type AlternatingPulse struct{}
type Breathing struct{}
type Fade struct{}
type Sweep struct{}

// Pulse alternates two colours. A single colour left brightness as the only cue
// that a beat had passed, which is invisible whenever a track's energy barely
// moves between beats. Two rather than three keeps it calmer than
// AlternatingPulse, which is what the busier sections use.
func (Pulse) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, rendering.IntentPulse, []palette.Color{ctx.Palette.Primary(), ctx.Palette.Secondary()})
}

func (AlternatingPulse) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, rendering.IntentPulse, []palette.Color{ctx.Palette.Primary(), ctx.Palette.Secondary(), ctx.Palette.Accent()})
}

func (Sweep) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, rendering.IntentSweep, []palette.Color{ctx.Palette.Secondary(), ctx.Palette.Accent(), ctx.Palette.Primary()})
}

func (Breathing) Generate(ctx Context) []timeline.Event {
	step := int64(2000)
	if ctx.Section.Energy > 0.55 {
		step = 1250
	}
	var events []timeline.Event
	for t := ctx.Section.StartMS; t < ctx.Section.EndMS; t += step {
		energy := energyAt(ctx.Energy, t, ctx.Section.Energy)
		brightness := smoothBrightness(energy, ctx.MinBright, ctx.MaxBright) * 0.82
		target := targetAt(ctx.Targets, int((t-ctx.Section.StartMS)/step))
		events = append(events, render(t, target, rendering.IntentGradient, ctx.Palette.Primary(), brightness, ctx.DurationMS*3, 0, ctx)...)
	}
	return events
}

func (Fade) Generate(ctx Context) []timeline.Event {
	target := targetAt(ctx.Targets, 0)
	return render(ctx.Section.StartMS, target, rendering.IntentGradient, ctx.Palette.Secondary(), smoothBrightness(ctx.Section.Energy, ctx.MinBright, ctx.MaxBright)*0.7, ctx.DurationMS*4, 0, ctx)
}

func pulseEvents(ctx Context, kind rendering.IntentKind, colors []palette.Color) []timeline.Event {
	step := ctx.BeatStep
	if step <= 0 {
		step = 1
	}

	beats := beatsInSection(ctx)
	var events []timeline.Event
	for beatIndex, beat := range beats {
		if beatIndex%step != 0 {
			continue
		}

		energy := energyAt(ctx.Energy, beat, ctx.Section.Energy)
		level := smoothBrightness(energy, ctx.MinBright, ctx.MaxBright)
		envelope := envelopeFor(level, beatIndex, beatGapMS(beats, beatIndex, step, ctx.Section.EndMS), ctx)

		for targetIndex := range targetsPerBeat(ctx.Targets) {
			target := targetAt(ctx.Targets, beatIndex+ctx.TargetShift+targetIndex)
			targetColor := colors[(beatIndex/step+targetIndex)%len(colors)]

			events = append(events, render(beat, target, kind, targetColor, envelope.peak, envelope.riseMS, beatIndex+targetIndex, ctx)...)
			if envelope.decays() {
				events = append(events, render(beat+envelope.riseMS, target, kind, targetColor, envelope.rest, envelope.fallMS, beatIndex+targetIndex, ctx)...)
			}
		}
	}
	return events
}

func beatsInSection(ctx Context) []int64 {
	var beats []int64
	for _, beat := range ctx.Beats {
		if beat < ctx.Section.StartMS || beat >= ctx.Section.EndMS {
			continue
		}
		beats = append(beats, beat)
	}
	return beats
}

// beatGapMS is the room until the next beat this effect will actually emit,
// which is step beats away rather than the next beat in the track.
func beatGapMS(beats []int64, beatIndex, step int, sectionEndMS int64) int64 {
	next := sectionEndMS
	if upcoming := beatIndex + step; upcoming < len(beats) {
		next = beats[upcoming]
	}
	gap := next - beats[beatIndex]
	if gap < 0 {
		return 0
	}
	return gap
}

// envelope shapes one beat as a fast rise to a peak followed by a fall back to a
// resting level, so the beat reads as a hit rather than a change of level. A
// single event per beat only sets a level and holds it, which is indistinguishable
// from the previous beat whenever consecutive levels are close.
type envelope struct {
	peak   float64
	rest   float64
	riseMS int64
	fallMS int64
}

// decays reports whether the fall is worth its own event. Skipped when the two
// levels are too close to tell apart, or the fall is shorter than the timeline
// normaliser would keep.
func (e envelope) decays() bool {
	return e.fallMS >= minFallMS && e.peak-e.rest >= minEnvelopeContrast
}

const (
	// minFallMS stays above the duration below which normalizeTimelineEvents
	// discards an event entirely.
	minFallMS           int64 = 25
	minRiseMS           int64 = 35
	minEnvelopeContrast       = 0.03
	restRatio                 = 0.55
)

func envelopeFor(level float64, beatIndex int, gapMS int64, ctx Context) envelope {
	peak := clamp(level*accentFor(beatIndex), ctx.MinBright, 1)

	// Rise fast, then spend the rest of the beat falling. Capping the rise by the
	// style's own duration keeps gentle styles gentle.
	riseMS := min(ctx.DurationMS, gapMS/4)
	if riseMS < minRiseMS {
		riseMS = min(minRiseMS, gapMS)
	}

	return envelope{
		peak:   peak,
		rest:   clamp(peak*restRatio, ctx.MinBright, 1),
		riseMS: max(riseMS, 1),
		fallMS: gapMS - riseMS,
	}
}

// accentFor emphasises the downbeat of each bar of four. It gives a pulse shape
// even where the track's own energy is flat, which is the usual case on a heavily
// compressed master.
func accentFor(beatIndex int) float64 {
	switch beatIndex % 4 {
	case 0:
		return 1.0
	case 2:
		return 0.9
	default:
		return 0.8
	}
}

func targetsPerBeat(targets []Target) []int {
	if len(targets) == 0 {
		return []int{0}
	}
	out := make([]int, len(targets))
	for i := range out {
		out[i] = i
	}
	return out
}

func render(timeMS int64, target Target, kind rendering.IntentKind, color palette.Color, brightness float64, durationMS int64, beatIndex int, ctx Context) []timeline.Event {
	return rendering.Render(rendering.EffectIntent{
		Kind:        kind,
		TimeMS:      timeMS,
		Target:      target.DeviceID,
		Color:       color,
		Palette:     ctx.Palette,
		Brightness:  brightness,
		DurationMS:  durationMS,
		BeatIndex:   beatIndex,
		Section:     string(ctx.Section.Type),
		DeviceIndex: target.Index,
		DeviceTotal: target.Total,
		Supported:   rendering.SupportedDeviceKinds{SingleZone: true, MultiZone: true, Matrix: true},
	}, devices.DeviceInfo{ID: target.DeviceID, Capabilities: target.Capabilities})
}

func targetAt(targets []Target, index int) Target {
	if len(targets) == 0 {
		return Target{DeviceID: "all", Total: 1, Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true}}
	}
	return targets[index%len(targets)]
}

func energyAt(points []analysis.EnergyPoint, timeMS int64, fallback float64) float64 {
	if len(points) == 0 {
		return fallback
	}
	best := points[0]
	for _, point := range points {
		if point.TimeMS > timeMS {
			break
		}
		best = point
	}
	return best.Value
}

func smoothBrightness(energy, minBright, maxBright float64) float64 {
	energy = clamp(energy, 0, 1)
	smoothed := energy * energy * (3 - 2*energy)
	return minBright + (maxBright-minBright)*smoothed
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
