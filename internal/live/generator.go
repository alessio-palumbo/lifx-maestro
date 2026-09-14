package live

import (
	"fmt"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/rendering"
	"lifx-maestro/internal/styles"
	"lifx-maestro/internal/timeline"
)

type GeneratorConfig struct {
	Style      string
	Intensity  generation.DynamicsOverride
	Devices    []devices.DeviceInfo
	AmbientHop time.Duration
}

type Generator struct {
	style       styles.Style
	intensity   generation.DynamicsOverride
	devices     []devices.DeviceInfo
	ambientHop  time.Duration
	lastAmbient time.Duration
	lastAccent  time.Duration
	beatIndex   int
	phraseIndex int
	motion      float64
	lastStateAt time.Duration
}

func NewGenerator(config GeneratorConfig) (*Generator, error) {
	if config.Style == "" {
		config.Style = "synthwave"
	}
	style, err := styles.Get(config.Style)
	if err != nil {
		return nil, err
	}
	if err := generation.ValidateDynamics(config.Intensity); err != nil {
		return nil, err
	}
	style = liveStyle(style, config.Intensity)
	return &Generator{
		style:      style,
		intensity:  config.Intensity,
		devices:    config.Devices,
		ambientHop: config.AmbientHop,
	}, nil
}

func (g *Generator) Generate(state State) []timeline.Event {
	motion := g.advanceMotion(state)
	if !state.Active || len(g.devices) == 0 {
		return nil
	}

	if state.SectionChange {
		g.phraseIndex++
	}
	ambientHop, accentGap, _ := g.pacing(state)
	accent := state.Onset || state.Beat || state.SectionChange
	if accent && !state.SectionChange && g.lastAccent > 0 && state.At-g.lastAccent < accentGap {
		accent = false
	}
	if !accent && !state.Sustained {
		return nil
	}
	if !accent && state.At-g.lastAmbient < ambientHop {
		return nil
	}
	if !accent {
		g.lastAmbient = state.At
	}

	kind := g.ambientIntent(state)
	color := bandColor(g.style.Palette, state)
	duration := ambientHop * 2
	brightnessScale := g.style.BrightnessScale * g.autoBrightnessScale(state)
	brightness := clamp((0.12+state.Energy*0.68)*brightnessScale, 0.01, 1)
	effectIndex := g.beatIndex
	if accent {
		g.lastAccent = state.At
		kind = g.accentIntent(state)
		color = g.style.Palette.AccentForBeat(effectIndex)
		brightness = clamp((0.28+state.Energy*0.82)*brightnessScale, 0.01, 1)
		duration = liveAccentDuration(state.TempoBPM, g.style.TransitionAggressiveness)
		g.beatIndex++
	}

	var events []timeline.Event
	for index, device := range g.devices {
		spatialIndex := effectIndex
		spatialPhase := 0.0
		if device.Capabilities.Kind == devices.DeviceKindMultiZone || device.Capabilities.Kind == devices.DeviceKindMatrix {
			spatialIndex = int(motion)
			spatialPhase = motion - float64(spatialIndex)
		}
		events = append(events, rendering.Render(rendering.EffectIntent{
			Kind:        kind,
			TimeMS:      state.At.Milliseconds(),
			Target:      device.ID,
			Color:       color,
			Palette:     g.style.Palette,
			Brightness:  brightness,
			DurationMS:  duration.Milliseconds(),
			BeatIndex:   spatialIndex + index,
			Phase:       spatialPhase,
			Section:     "live",
			DeviceIndex: index,
			DeviceTotal: len(g.devices),
			Supported: rendering.SupportedDeviceKinds{
				SingleZone: true,
				MultiZone:  true,
				Matrix:     true,
			},
		}, device)...)
	}
	return events
}

func (g *Generator) ambientIntent(state State) rendering.IntentKind {
	switch state.Dynamics {
	case DynamicsEnergetic:
		if g.phraseIndex%2 == 1 {
			return rendering.IntentSweep
		}
		return rendering.IntentMatrixWave
	case DynamicsBalanced:
		if g.phraseIndex%2 == 1 {
			return rendering.IntentSweep
		}
	}
	return rendering.IntentGradient
}

func (g *Generator) accentIntent(state State) rendering.IntentKind {
	if state.SectionChange {
		return rendering.IntentPulse
	}
	if state.Dynamics == DynamicsEnergetic && g.beatIndex%2 == 1 {
		return rendering.IntentSweep
	}
	if state.High > state.Low*1.15 && state.High > state.Mid*1.1 {
		return rendering.IntentSweep
	}
	return rendering.IntentPulse
}

func (g *Generator) advanceMotion(state State) float64 {
	if g.lastStateAt == 0 || state.At <= g.lastStateAt {
		g.lastStateAt = state.At
		return g.motion
	}
	delta := state.At - g.lastStateAt
	g.lastStateAt = state.At
	if delta > time.Second {
		delta = time.Second
	}
	bpm := state.TempoBPM
	if bpm < 40 || bpm > 240 {
		bpm = 90
	}
	_, _, motionScale := g.pacing(state)
	g.motion += delta.Seconds() * bpm / 60 * motionScale
	return g.motion
}

func (g *Generator) pacing(state State) (time.Duration, time.Duration, float64) {
	var ambient time.Duration
	var accent time.Duration
	var motion float64
	if g.intensity != "" && g.intensity != generation.DynamicsAuto {
		ambient, accent, motion = livePacing(g.intensity)
	} else {
		switch state.Dynamics {
		case DynamicsEnergetic:
			ambient, accent, motion = livePacing(generation.DynamicsEnergetic)
		case DynamicsBalanced:
			ambient, accent, motion = livePacing(generation.DynamicsBalanced)
		default:
			ambient, accent, motion = livePacing(generation.DynamicsCalm)
		}
	}
	if g.ambientHop > 0 {
		ambient = g.ambientHop
	}
	return ambient, accent, motion
}

func (g *Generator) autoBrightnessScale(state State) float64 {
	if g.intensity != "" && g.intensity != generation.DynamicsAuto {
		return 1
	}
	switch state.Dynamics {
	case DynamicsEnergetic:
		return 1
	case DynamicsBalanced:
		return 0.9
	default:
		return 0.78
	}
}

func livePacing(intensity generation.DynamicsOverride) (time.Duration, time.Duration, float64) {
	switch intensity {
	case generation.DynamicsCalm:
		return 400 * time.Millisecond, 600 * time.Millisecond, 0.55
	case generation.DynamicsEnergetic:
		return 200 * time.Millisecond, 120 * time.Millisecond, 1
	case "", generation.DynamicsAuto, generation.DynamicsBalanced:
		return 250 * time.Millisecond, 300 * time.Millisecond, 0.8
	default:
		panic(fmt.Sprintf("validated intensity %q became unsupported", intensity))
	}
}

func liveStyle(style styles.Style, intensity generation.DynamicsOverride) styles.Style {
	switch intensity {
	case generation.DynamicsCalm:
		style.BrightnessScale *= 0.78
		style.TransitionAggressiveness *= 0.55
	case generation.DynamicsBalanced:
		style.BrightnessScale *= 0.9
		style.TransitionAggressiveness *= 0.8
	case generation.DynamicsEnergetic:
		// Keep the selected style at full force.
	case "", generation.DynamicsAuto:
		// Live has no complete-track profile; Auto follows the measured state.
	default:
		panic(fmt.Sprintf("validated intensity %q became unsupported", intensity))
	}
	return style
}

func bandColor(colors palette.Palette, state State) palette.Color {
	switch {
	case state.Low >= state.Mid && state.Low >= state.High:
		return colors.Primary()
	case state.High >= state.Mid:
		return colors.Accent()
	default:
		return colors.Secondary()
	}
}

func liveAccentDuration(bpm, aggression float64) time.Duration {
	beat := 500 * time.Millisecond
	if bpm >= 40 && bpm <= 240 {
		beat = time.Duration(float64(time.Minute) / bpm)
	}
	factor := 0.42 - clamp(aggression, 0, 1)*0.22
	duration := time.Duration(float64(beat) * factor)
	if duration < 55*time.Millisecond {
		return 55 * time.Millisecond
	}
	if duration > 350*time.Millisecond {
		return 350 * time.Millisecond
	}
	return duration
}
