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
	devices     []devices.DeviceInfo
	ambientHop  time.Duration
	lastAmbient time.Duration
	beatIndex   int
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
	if config.AmbientHop <= 0 {
		config.AmbientHop = 200 * time.Millisecond
	}
	style = liveStyle(style, config.Intensity)
	return &Generator{style: style, devices: config.Devices, ambientHop: config.AmbientHop}, nil
}

func (g *Generator) Generate(state State) []timeline.Event {
	if !state.Active || len(g.devices) == 0 {
		return nil
	}

	accent := state.Onset || state.Beat
	if !accent && state.At-g.lastAmbient < g.ambientHop {
		return nil
	}
	if !accent {
		g.lastAmbient = state.At
	}

	kind := rendering.IntentGradient
	color := bandColor(g.style.Palette, state)
	duration := g.ambientHop * 2
	brightness := clamp((0.12+state.Energy*0.68)*g.style.BrightnessScale, 0.01, 1)
	effectIndex := g.beatIndex
	if accent {
		kind = rendering.IntentPulse
		if state.High > state.Low*1.15 && state.High > state.Mid*1.1 {
			kind = rendering.IntentSweep
		}
		color = g.style.Palette.AccentForBeat(effectIndex)
		brightness = clamp((0.28+state.Energy*0.82)*g.style.BrightnessScale, 0.01, 1)
		duration = liveAccentDuration(state.TempoBPM, g.style.TransitionAggressiveness)
		g.beatIndex++
	}

	var events []timeline.Event
	for index, device := range g.devices {
		events = append(events, rendering.Render(rendering.EffectIntent{
			Kind:        kind,
			TimeMS:      state.At.Milliseconds(),
			Target:      device.ID,
			Color:       color,
			Palette:     g.style.Palette,
			Brightness:  brightness,
			DurationMS:  duration.Milliseconds(),
			BeatIndex:   effectIndex + index,
			Phase:       0,
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
