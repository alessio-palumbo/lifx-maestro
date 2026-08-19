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
	// sweepZonesPerBeat moves a sweep faster than one zone per beat, so it crosses
	// a long strip within a musical phrase rather than crawling.
	sweepZonesPerBeat = 2
)

// multiZonePulseFrame lights a head zone that advances one zone per beat and
// trails a fading tail behind it. This replaces a static gradient, which changed
// only in brightness and so looked frozen on a strip.
func multiZonePulseFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	caps := frameCapabilities(intent, surface, width, height)
	size := caps.Width * caps.Height
	if size <= 0 {
		size = 1
	}

	accent := intent.Color
	if accent.Kelvin == 0 {
		accent = intent.Palette.AccentForBeat(intent.BeatIndex)
	}
	background := intent.Palette.BackgroundForSection(intent.Section)

	head := positiveModulo(intent.BeatIndex, size)
	tail := max(2, size/4)

	colors := make([]lifxeffects.Color, size)
	for i := range colors {
		behind := positiveModulo(head-i, size)
		if behind >= tail {
			colors[i] = effectColor(background, intent.Brightness*backgroundLevel)
			continue
		}
		level := 1 - float64(behind)/float64(tail)
		colors[i] = effectColor(accent, intent.Brightness*(tailFloor+(1-tailFloor)*level))
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
	background := intent.Palette.BackgroundForSection(intent.Section)
	head := positiveModulo(intent.BeatIndex*sweepZonesPerBeat, size)

	colors := make([]lifxeffects.Color, size)
	for i := range colors {
		behind := positiveModulo(head-i, size)
		if behind >= band {
			colors[i] = effectColor(background, intent.Brightness*backgroundLevel)
			continue
		}
		// Fade the trailing edge so the band has a direction.
		level := 1 - 0.55*float64(behind)/float64(band)
		colors[i] = effectColor(stops[positiveModulo(behind, len(stops))], intent.Brightness*level)
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
	// One ring per beat, restarting at the centre once it leaves the surface.
	steps := int(maxRadius) + 1
	radius := float64(positiveModulo(intent.BeatIndex, steps))

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
