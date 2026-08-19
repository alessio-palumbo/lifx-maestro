package rendering

import (
	"math"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"lifx-maestro/internal/palette"
)

// Spatial effects give strips and tiles something that moves across the surface
// rather than a whole-device colour that only changes brightness. Position is
// derived from the beat index, so motion stays locked to the music.
const (
	// backgroundLevel dims the unlit part of a surface instead of switching it
	// off, so a strip still reads as lit between hits.
	backgroundLevel = 0.22
	// tailFloor keeps the far end of a tail visible rather than fading to nothing.
	tailFloor = 0.15
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
	// waveSharpness tightens the crest. 1 is a plain cosine, which is the most
	// fluid: raising it flattens the trough into a dead stretch, measurably so at
	// 16 zones, where 2.2 parks 31% of them at the floor against 6% at 1. Beat
	// definition comes from the envelope scaling the whole frame, so the wave does
	// not need a tight peak to land.
	waveSharpness = 1.0
)

// headPosition is where a travelling effect has reached, in fractional zones. The
// phase keeps it moving between beats, so the decay half of a beat's envelope is
// drawn slightly further along than the hit.
func headPosition(intent EffectIntent, size, beatsPerCrossing int) float64 {
	perBeat := float64(size) / float64(beatsPerCrossing)
	if perBeat < 1 {
		perBeat = 1
	}
	return (float64(intent.BeatIndex) + intent.Phase) * perBeat
}

// distanceBehind measures how far a zone sits behind the head, wrapping around
// the surface, in fractional zones.
func distanceBehind(head float64, index, size int) float64 {
	behind := math.Mod(head-float64(index), float64(size))
	if behind < 0 {
		behind += float64(size)
	}
	return behind
}

// multiZonePulseFrame lights a head zone that advances one zone per beat and
// trails a fading tail behind it. This replaces a static gradient, which changed
// only in brightness and so looked frozen on a strip.
func multiZonePulseFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	size := caps.Width * caps.Height
	if size <= 0 {
		size = 1
	}

	// Every zone belongs to the effect: colour flows along the strip and brightness
	// rides a crest that travels with the beat.
	//
	// A lit head over a static background could not work here. Because the head
	// advances a whole fraction of the strip per beat it only ever landed on a few
	// zones — 8 of 16, never the odd ones — so it appeared to sit in the same spots
	// while most of the strip held one flat colour.
	stops := intent.Palette.GradientStops(size)
	if len(stops) == 0 {
		stops = []palette.Color{intent.Color}
	}

	head := headPosition(intent, size, beatsPerTraversal)
	colors := make([]lifxeffects.Color, size)
	for i := range colors {
		// Colours scroll with the head, so no zone keeps the same hue.
		stop := stops[positiveModulo(i+int(head), len(stops))]

		// One cosine cycle across the strip, its crest on the head. Every zone
		// changes as the crest moves, rather than only the few under a tail.
		offset := distanceBehind(head, i, size) / float64(size)
		crest := 0.5 * (1 + math.Cos(2*math.Pi*offset))
		level := waveFloor + (1-waveFloor)*math.Pow(crest, waveSharpness)

		colors[i] = effectColor(stop, intent.Brightness*level)
	}

	return frame(colors, caps, intent.DurationMS)
}

// multiZoneSweepFrame travels a band of palette colours along the strip. The
// previous sweep lit a single zone, which read as a dot rather than a sweep.
func multiZoneSweepFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	size := caps.Width * caps.Height
	if size <= 0 {
		size = 1
	}

	band := max(2, size/3)
	stops := intent.Palette.GradientStops(band)
	if len(stops) == 0 {
		stops = []palette.Color{intent.Color}
	}
	// Behind the band the strip carries a dimmer version of the same flowing
	// gradient. A single background colour there left two thirds of the strip
	// holding one flat hue while the band went past.
	trail := intent.Palette.GradientStops(size)
	if len(trail) == 0 {
		trail = []palette.Color{intent.Palette.BackgroundForSection(intent.Section)}
	}
	head := headPosition(intent, size, sweepBeatsPerTraversal)

	colors := make([]lifxeffects.Color, size)
	for i := range colors {
		behind := distanceBehind(head, i, size)
		if behind >= float64(band) {
			stop := trail[positiveModulo(i+int(head), len(trail))]
			colors[i] = effectColor(stop, intent.Brightness*backgroundLevel)
			continue
		}
		// Fade the trailing edge so the band has a direction.
		level := 1 - 0.55*behind/float64(band)
		colors[i] = effectColor(stops[min(int(behind), len(stops)-1)], intent.Brightness*level)
	}

	return frame(colors, caps, intent.DurationMS)
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

	colors := make([]lifxeffects.Color, 0, caps.Width*caps.Height)
	for y := 0; y < caps.Height; y++ {
		for x := 0; x < caps.Width; x++ {
			distance := math.Abs(math.Hypot(float64(x)-cx, float64(y)-cy) - radius)
			if distance >= ringWidth {
				colors = append(colors, effectColor(background, intent.Brightness*backgroundLevel))
				continue
			}
			level := 1 - distance/ringWidth
			colors = append(colors, effectColor(accent, intent.Brightness*(tailFloor+(1-tailFloor)*level)))
		}
	}

	return frame(colors, caps, intent.DurationMS)
}

// matrixWaveFrame scrolls palette colours diagonally. Shifting columns alone left
// every row identical, so the tile read as a set of vertical bars.
func matrixWaveFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)

	stops := intent.Palette.GradientStops(caps.Width + caps.Height)
	if len(stops) == 0 {
		stops = []palette.Color{intent.Color}
	}

	colors := make([]lifxeffects.Color, 0, caps.Width*caps.Height)
	for y := 0; y < caps.Height; y++ {
		for x := 0; x < caps.Width; x++ {
			color := stops[positiveModulo(x+y+intent.BeatIndex, len(stops))]
			colors = append(colors, effectColor(color, intent.Brightness))
		}
	}

	return frame(colors, caps, intent.DurationMS)
}

// driftFrame rotates a frame's colours by the beat index so an otherwise static
// gradient keeps moving. Used by the calm sections, where a travelling head would
// be too busy but a frozen surface looks broken.
func driftFrame(source lifxeffects.Frame, beatIndex int) lifxeffects.Frame {
	if len(source.Colors) <= 1 || beatIndex == 0 {
		return source
	}
	offset := positiveModulo(beatIndex, len(source.Colors))
	rotated := make([]lifxeffects.Color, len(source.Colors))
	for i := range source.Colors {
		rotated[i] = source.Colors[positiveModulo(i+offset, len(source.Colors))]
	}
	source.Colors = rotated
	return source
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
