package rendering

import (
	"math"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/timeline"
)

type IntentKind string

const (
	IntentSolid      IntentKind = "solid"
	IntentPulse      IntentKind = "pulse"
	IntentGradient   IntentKind = "gradient"
	IntentSweep      IntentKind = "sweep"
	IntentBands      IntentKind = "bands"
	IntentMatrixWave IntentKind = "matrix_wave"
)

type SupportedDeviceKinds struct {
	SingleZone bool
	MultiZone  bool
	Matrix     bool
}

type EffectIntent struct {
	Kind        IntentKind
	TimeMS      int64
	Target      string
	Color       palette.Color
	Palette     palette.Palette
	Brightness  float64
	DurationMS  int64
	BeatIndex   int
	Section     string
	DeviceIndex int
	DeviceTotal int
	Supported   SupportedDeviceKinds
}

type Renderer interface {
	Render(effect EffectIntent, device devices.DeviceInfo) []timeline.Event
}

type SingleZoneRenderer struct{}
type MultiZoneRenderer struct{}
type MatrixRenderer struct{}

func Render(intent EffectIntent, device devices.DeviceInfo) []timeline.Event {
	switch device.Capabilities.Kind {
	case devices.DeviceKindMultiZone:
		return MultiZoneRenderer{}.Render(intent, device)
	case devices.DeviceKindMatrix:
		return MatrixRenderer{}.Render(intent, device)
	default:
		return SingleZoneRenderer{}.Render(intent, device)
	}
}

func (SingleZoneRenderer) Render(intent EffectIntent, device devices.DeviceInfo) []timeline.Event {
	color := intent.Color
	if color.Kelvin == 0 {
		color = intent.Palette.ColorForDevice(intent.DeviceIndex, intent.DeviceTotal)
	} else if intent.DeviceTotal > 1 {
		color = intent.Palette.ColorForDevice(intent.DeviceIndex+intent.BeatIndex, intent.DeviceTotal)
	}
	return []timeline.Event{setColor(intent.TimeMS, device.ID, color, intent.Brightness, intent.DurationMS)}
}

func (MultiZoneRenderer) Render(intent EffectIntent, device devices.DeviceInfo) []timeline.Event {
	if !intent.Supported.MultiZone {
		return SingleZoneRenderer{}.Render(intent, device)
	}
	count := zoneCount(device)

	var colors []palette.Color
	switch intent.Kind {
	case IntentSweep:
		frame := sweepFrame(intent, surfaceForMultiZone(device, count), count, 1)
		deviceFrames := adaptFrame(frame, surfaceForMultiZone(device, count))
		if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
			return []timeline.Event{zoneColorsEvent(intent, device.ID, deviceFrames[0])}
		}
		colors = sweepStops(intent, count)
	case IntentBands:
		frame := gradientFrame(intent, surfaceForMultiZone(device, count), count, 1)
		deviceFrames := adaptFrame(frame, surfaceForMultiZone(device, count))
		if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
			return []timeline.Event{zoneColorsEvent(intent, device.ID, deviceFrames[0])}
		}
		colors = intent.Palette.GradientStops(count)
	default:
		frame := gradientFrame(intent, surfaceForMultiZone(device, count), count, 1)
		deviceFrames := adaptFrame(frame, surfaceForMultiZone(device, count))
		if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
			return []timeline.Event{zoneColorsEvent(intent, device.ID, deviceFrames[0])}
		}
		colors = gradientStops(intent, count)
	}

	frame := frameFromPaletteColors(colors, count, 1, intent.Brightness, intent.DurationMS)
	deviceFrames := adaptFrame(frame, surfaceForMultiZone(device, count))
	if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
		return []timeline.Event{zoneColorsEvent(intent, device.ID, deviceFrames[0])}
	}

	zones := zoneColorParams(colors, intent.Brightness)
	return []timeline.Event{{
		TimeMS: avoidZero(intent.TimeMS),
		Target: device.ID,
		Action: "set_zone_colors",
		Params: timeline.MustParams(timeline.SetZoneColorsParams{DurationMS: intent.DurationMS, Zones: zones}),
	}}
}

func (MatrixRenderer) Render(intent EffectIntent, device devices.DeviceInfo) []timeline.Event {
	if !intent.Supported.Matrix {
		return SingleZoneRenderer{}.Render(intent, device)
	}
	width, height := matrixDimensions(device)

	if intent.Kind == IntentPulse {
		frame := matrixPulseFrame(intent, surfaceForMatrix(device, width, height), width, height)
		deviceFrames := adaptFrame(frame, surfaceForMatrix(device, width, height))
		if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
			return []timeline.Event{matrixColorsEvent(intent, device.ID, deviceFrames[0])}
		}
	}

	if intent.Kind != IntentSweep && intent.Kind != IntentMatrixWave {
		frame := gradientFrame(intent, surfaceForMatrix(device, width, height), width, height)
		deviceFrames := adaptFrame(frame, surfaceForMatrix(device, width, height))
		if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
			return []timeline.Event{matrixColorsEvent(intent, device.ID, deviceFrames[0])}
		}
	}

	colors := make([]palette.Color, 0, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			colors = append(colors, matrixColor(intent, x, y, width, height))
		}
	}

	frame := frameFromPaletteColors(colors, width, height, intent.Brightness, intent.DurationMS)
	deviceFrames := adaptFrame(frame, surfaceForMatrix(device, width, height))
	if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
		return []timeline.Event{matrixColorsEvent(intent, device.ID, deviceFrames[0])}
	}

	pixels := matrixColorParams(colors, width, height, intent.Brightness)
	return []timeline.Event{{
		TimeMS: avoidZero(intent.TimeMS),
		Target: device.ID,
		Action: "set_matrix_colors",
		Params: timeline.MustParams(timeline.SetMatrixColorsParams{
			Width: width, Height: height, DurationMS: intent.DurationMS, Pixels: pixels,
		}),
	}}
}

func setColor(timeMS int64, target string, color palette.Color, brightness float64, durationMS int64) timeline.Event {
	return timeline.Event{
		TimeMS: avoidZero(timeMS),
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

func gradientStops(intent EffectIntent, count int) []palette.Color {
	stops := intent.Palette.GradientStops(count)
	if len(stops) == 0 {
		stops = []palette.Color{intent.Color}
	}
	return stops
}

func sweepStops(intent EffectIntent, count int) []palette.Color {
	base := intent.Palette.BackgroundForSection(intent.Section)
	accent := intent.Palette.AccentForBeat(intent.BeatIndex)
	colors := make([]palette.Color, count)
	for i := range colors {
		colors[i] = base
	}
	if count > 0 {
		colors[intent.BeatIndex%count] = accent
	}
	return colors
}

func matrixColor(intent EffectIntent, x, y, width, height int) palette.Color {
	switch intent.Kind {
	case IntentSweep, IntentMatrixWave:
		stops := intent.Palette.GradientStops(width)
		return stops[(x+intent.BeatIndex)%len(stops)]
	case IntentPulse:
		cx := float64(width-1) / 2
		cy := float64(height-1) / 2
		dist := math.Hypot(float64(x)-cx, float64(y)-cy)
		if int(dist+float64(intent.BeatIndex))%3 == 0 {
			return intent.Palette.AccentForBeat(intent.BeatIndex)
		}
		return intent.Palette.BackgroundForSection(intent.Section)
	default:
		stops := intent.Palette.GradientStops(height)
		return stops[y%len(stops)]
	}
}

func colorValue(color palette.Color, brightness float64) timeline.ColorValue {
	return timeline.ColorValue{
		Hue:        math.Round(color.Hue*10) / 10,
		Saturation: math.Round(color.Saturation*1000) / 1000,
		Brightness: math.Round(clamp(brightness, 0, 1)*1000) / 1000,
		Kelvin:     color.Kelvin,
	}
}

func zoneColorParams(colors []palette.Color, brightness float64) []timeline.ZoneColorParams {
	zones := make([]timeline.ZoneColorParams, len(colors))
	for i, color := range colors {
		zones[i] = timeline.ZoneColorParams{Index: i, Color: colorValue(color, brightness)}
	}
	return zones
}

func matrixColorParams(colors []palette.Color, width, height int, brightness float64) []timeline.MatrixColorParams {
	pixels := make([]timeline.MatrixColorParams, 0, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			if i >= len(colors) {
				continue
			}
			pixels = append(pixels, timeline.MatrixColorParams{X: x, Y: y, Color: colorValue(colors[i], brightness)})
		}
	}
	return pixels
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
