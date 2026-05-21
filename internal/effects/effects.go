package effects

import (
	"math"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
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
	return pulseEvents(ctx, []palette.Color{ctx.Palette.Primary()})
}

func (AlternatingPulse) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, []palette.Color{ctx.Palette.Primary(), ctx.Palette.Secondary(), ctx.Palette.Accent()})
}

func (Sweep) Generate(ctx Context) []timeline.Event {
	return pulseEvents(ctx, []palette.Color{ctx.Palette.Secondary(), ctx.Palette.Accent(), ctx.Palette.Primary()})
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
		events = append(events, colorEvent(t, target.DeviceID, ctx.Palette.Primary(), brightness, ctx.DurationMS*3))
	}
	return events
}

func (Fade) Generate(ctx Context) []timeline.Event {
	return []timeline.Event{
		colorEvent(ctx.Section.StartMS, targetAt(ctx.Targets, 0).DeviceID, ctx.Palette.Secondary(), smoothBrightness(ctx.Section.Energy, ctx.MinBright, ctx.MaxBright)*0.7, ctx.DurationMS*4),
	}
}

func pulseEvents(ctx Context, colors []palette.Color) []timeline.Event {
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

		target := targetAt(ctx.Targets, beatIndex+ctx.TargetShift)
		color := colors[(beatIndex/step)%len(colors)]
		energy := energyAt(ctx.Energy, beat, ctx.Section.Energy)
		brightness := smoothBrightness(energy, ctx.MinBright, ctx.MaxBright)
		events = append(events, colorEvent(avoidZero(beat), target.DeviceID, color, brightness, ctx.DurationMS))
		beatIndex++
	}
	return events
}

func colorEvent(timeMS int64, target string, color palette.Color, brightness float64, durationMS int64) timeline.Event {
	return timeline.Event{
		TimeMS: timeMS,
		Target: target,
		Action: "set_color",
		Params: timeline.MustParams(timeline.SetColorParams{
			Hue:        ptr(math.Round(color.Hue*10) / 10),
			Saturation: ptr(math.Round(color.Saturation*1000) / 1000),
			Brightness: ptr(math.Round(clamp(brightness, 0, 1)*1000) / 1000),
			Kelvin:     ptr(color.Kelvin),
			DurationMS: ptr(durationMS),
		}),
	}
}

func targetAt(targets []Target, index int) Target {
	if len(targets) == 0 {
		return Target{DeviceID: "all", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true, HasKelvin: true}}
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

func avoidZero(timeMS int64) int64 {
	if timeMS == 0 {
		return 1
	}
	return timeMS
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

func ptr[T any](value T) *T {
	return &value
}
