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
	case devices.DeviceKindSingleZone:
		return SingleZoneRenderer{}.Render(intent, device)
	default:
		return nil
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
	surface := surfaceForMultiZone(device, count)

	var frame effectFrame
	switch intent.Kind {
	case IntentSweep:
		frame = sweepFrame(intent, surface, count, 1)
	default:
		frame = gradientFrame(intent, surface, count, 1)
	}

	deviceFrames := adaptFrame(frame, surface)
	if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
		return []timeline.Event{zoneColorsEvent(intent, device.ID, deviceFrames[0])}
	}
	return nil
}

func (MatrixRenderer) Render(intent EffectIntent, device devices.DeviceInfo) []timeline.Event {
	if !intent.Supported.Matrix {
		return SingleZoneRenderer{}.Render(intent, device)
	}
	width, height := matrixDimensions(device)
	surface := surfaceForMatrix(device, width, height)

	var frame effectFrame
	switch intent.Kind {
	case IntentPulse:
		frame = matrixPulseFrame(intent, surface, width, height)
	case IntentSweep, IntentMatrixWave:
		frame = matrixWaveFrame(intent, surface, width, height)
	default:
		frame = gradientFrame(intent, surface, width, height)
	}

	deviceFrames := adaptFrame(frame, surface)
	if len(deviceFrames) > 0 && len(deviceFrames[0].Colors) > 0 {
		return []timeline.Event{matrixColorsEvent(intent, device.ID, deviceFrames[0])}
	}
	return nil
}

func setColor(timeMS int64, target string, color palette.Color, brightness float64, durationMS int64) timeline.Event {
	return timeline.Event{
		TimeMS: avoidZero(timeMS),
		Target: target,
		Action: "set_color",
		Params: timeline.MustParams(timeline.SetColorParams{
			Hue:        ptr(math.Round(color.Hue*10) / 10),
			Saturation: ptr(math.Round(clamp(color.Saturation, 0, 1)*1000) / 10),
			Brightness: ptr(math.Round(clamp(brightness, 0.01, 1)*1000) / 10),
			Kelvin:     ptr(color.Kelvin),
			DurationMS: ptr(durationMS),
		}),
	}
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
