package rendering

import (
	"math"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

// Spatial effects give strips and tiles something that moves across the surface
// rather than a whole-device colour that only changes brightness. Position is
// derived from the beat index, so motion stays locked to the music.
const (
	// backgroundLevel dims the unlit part of a surface instead of switching it
	// off, so a strip still reads as lit between hits.
	backgroundLevel = 0.22
	// sweepBandFraction, sweepBackgroundLevel, and sweepTailLevel are intentionally
	// tighter than PaletteSweep's defaults so sweep remains visually distinct from
	// Flow: a moving band over a dim background rather than another broad wave.
	sweepBandFraction    = 0.22
	sweepBackgroundLevel = 0.12
	sweepTailLevel       = 0.3
	// ringWidth is how many pixels either side of the ring radius stay lit.
	ringWidth = 1.6
	// beatsPerTraversal is how long a travelling effect takes to cross a surface,
	// in beats. Fixing the speed per beat instead made long strips crawl: at one
	// zone per beat a 16-zone strip took 16 beats to cross, so the light hung
	// around wherever it happened to be. Tying it to a bar means a strip of any
	// length crosses in the same musical time.
	beatsPerTraversal = 4
	// sweepBeatsPerTraversal crosses faster still, since a sweep is the punchier
	// effect and only the busy sections use it.
	sweepBeatsPerTraversal = 2
	// waveFloor is how lit the trough of a travelling wave stays. The whole strip
	// keeps some light so the motion reads as a wave over it rather than a dot
	// crossing a dead surface.
	waveFloor = 0.3
)

// multiZonePulseFrame uses Flow's broad travelling crest. Forward keeps the
// brightness movement aligned with Maestro's previous beat response; the palette
// sequence is only offset by one zone in some phases, which is not perceptible on
// a physical strip.
func multiZonePulseFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	flow := lifxeffects.NewFlow(lifxeffects.FlowConfig{
		Capabilities: caps,
		Palette:      effectPalette(intent.Palette, intent.Brightness),
		Axis:         lifxeffects.FlowAxisHorizontal,
		Direction:    lifxeffects.FlowDirectionForward,
		Floor:        waveFloor,
	})
	return flow.FrameAtPhase((float64(intent.BeatIndex)+intent.Phase)/beatsPerTraversal, time.Duration(intent.DurationMS)*time.Millisecond)
}

// multiZoneSweepFrame uses a tighter PaletteSweep than the library default. This
// keeps sweep distinct from pulse: a bright travelling band over a dim moving
// background.
func multiZoneSweepFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	sweep := lifxeffects.NewPaletteSweep(lifxeffects.PaletteSweepConfig{
		Capabilities:               caps,
		Palette:                    effectPalette(intent.Palette, intent.Brightness),
		Axis:                       lifxeffects.FlowAxisHorizontal,
		Direction:                  lifxeffects.FlowDirectionForward,
		BandFraction:               sweepBandFraction,
		BackgroundBrightnessFactor: sweepBackgroundLevel,
		TailBrightnessFactor:       sweepTailLevel,
		Sampling:                   lifxeffects.FlowSamplingStep,
	})
	return sweep.FrameAtPhase((float64(intent.BeatIndex)+intent.Phase)/sweepBeatsPerTraversal, time.Duration(intent.DurationMS)*time.Millisecond)
}

// matrixRingFrame expands a ring from the centre, one step per beat. The previous
// radial pulse used a hard modulo test, which produced fixed concentric stripes
// rather than a ring that travels outwards.
func matrixRingFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)

	accent := intent.Color
	if accent.Kelvin == 0 {
		accent = intent.Palette.AccentForBeat(intent.BeatIndex)
	}
	background := intent.Palette.BackgroundForSection(intent.Section)

	cx := float64(caps.Width-1) / 2
	cy := float64(caps.Height-1) / 2
	maxRadius := math.Hypot(cx, cy)
	// One ring per beat, restarting at the centre once it leaves the surface. The
	// phase carries it outwards between beats rather than jumping a whole step.
	steps := int(maxRadius) + 1
	radius := math.Mod(float64(intent.BeatIndex)+intent.Phase, float64(steps))
	span := maxRadius + ringWidth
	if span <= 0 {
		span = 1
	}

	ring := lifxeffects.NewRing(lifxeffects.RingConfig{
		Capabilities: caps,
		Palette: lifxeffects.Palette{
			Name:        intent.Palette.Name,
			Base:        effectColors(intent.Palette.Base, intent.Brightness),
			Accents:     []lifxeffects.Color{effectColor(accent, intent.Brightness)},
			Backgrounds: []lifxeffects.Color{effectColor(background, intent.Brightness)},
		},
		Width: ringWidth,
		Floor: backgroundLevel,
	})
	return ring.FrameAtPhase(radius/span, time.Duration(intent.DurationMS)*time.Millisecond)
}

// matrixWaveFrame scrolls palette colours diagonally. Shifting columns alone left
// every row identical, so the tile read as a set of vertical bars.
func matrixWaveFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	span := max(caps.Width+caps.Height-1, 1)
	flow := lifxeffects.NewFlow(lifxeffects.FlowConfig{
		Capabilities:   caps,
		Palette:        effectPalette(intent.Palette, intent.Brightness),
		Axis:           lifxeffects.FlowAxisDiagonal,
		Direction:      lifxeffects.FlowDirectionReverse,
		BrightnessMode: lifxeffects.FlowBrightnessConstant,
	})
	return flow.FrameAtPhase(float64(intent.BeatIndex)/float64(span), time.Duration(intent.DurationMS)*time.Millisecond)
}

// driftFrame keeps calm sections moving without adding a travelling brightness
// crest. Reverse matches Maestro's previous gradient rotation.
func driftFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	span := max(caps.Width*caps.Height, 1)
	drift := lifxeffects.NewGradientDrift(lifxeffects.GradientDriftConfig{
		Capabilities: caps,
		Palette:      effectPalette(intent.Palette, intent.Brightness),
		Axis:         lifxeffects.FlowAxisHorizontal,
		Direction:    lifxeffects.FlowDirectionReverse,
		Sampling:     lifxeffects.FlowSamplingStep,
	})
	return drift.FrameAtPhase(float64(intent.BeatIndex)/float64(span), time.Duration(intent.DurationMS)*time.Millisecond)
}

func frameCapabilities(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Capabilities {
	caps := effectCapabilities(surface, width, height)
	if caps.Width <= 0 {
		caps.Width = width
	}
	if caps.Height <= 0 {
		caps.Height = height
	}
	return caps
}

func frame(colors []lifxeffects.Color, caps lifxeffects.Capabilities, durationMS int64) lifxeffects.Frame {
	return lifxeffects.Frame{
		Colors:   colors,
		Width:    caps.Width,
		Height:   caps.Height,
		Duration: time.Duration(durationMS) * time.Millisecond,
	}
}
