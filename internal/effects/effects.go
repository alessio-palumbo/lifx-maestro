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

func (Pulse) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, rendering.IntentPulse, []palette.Color{ctx.Palette.Primary()})
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

	var events []timeline.Event
	var beatIndex int
	for _, beat := range ctx.Beats {
		if beat < ctx.Section.StartMS || beat >= ctx.Section.EndMS {
			continue
		}
		if beatIndex%step != 0 {
			beatIndex++
			continue
		}

		energy := energyAt(ctx.Energy, beat, ctx.Section.Energy)
		brightness := smoothBrightness(energy, ctx.MinBright, ctx.MaxBright)
		for targetIndex := range targetsPerBeat(ctx.Targets) {
			target := targetAt(ctx.Targets, beatIndex+ctx.TargetShift+targetIndex)
			targetColor := colors[(beatIndex/step+targetIndex)%len(colors)]
			events = append(events, render(beat, target, kind, targetColor, brightness, ctx.DurationMS, beatIndex+targetIndex, ctx)...)
		}
		beatIndex++
	}
	return events
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
