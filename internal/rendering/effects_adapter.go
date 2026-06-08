package rendering

import (
	"math"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/palette"
	"lifx-maestro/internal/timeline"
)

func zoneColorsEvent(intent EffectIntent, target string, frame lifxeffects.DeviceFrame) timeline.Event {
	return timeline.Event{
		TimeMS: avoidZero(intent.TimeMS),
		Target: target,
		Action: "set_zone_colors",
		Params: timeline.MustParams(timeline.SetZoneColorsParams{
			DurationMS: durationMS(frame, intent.DurationMS),
			Zones:      zoneColorParamsFromEffects(frame.Colors),
		}),
	}
}

func matrixColorsEvent(intent EffectIntent, target string, frame lifxeffects.DeviceFrame) timeline.Event {
	width := frame.SendWidth
	height := frame.Height
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = len(frame.Colors) / width
	}
	return timeline.Event{
		TimeMS: avoidZero(intent.TimeMS),
		Target: target,
		Action: "set_matrix_colors",
		Params: timeline.MustParams(timeline.SetMatrixColorsParams{
			Width:      width,
			Height:     height,
			DurationMS: durationMS(frame, intent.DurationMS),
			Pixels:     matrixColorParamsFromEffects(frame.Colors, width, height),
		}),
	}
}

func gradientFrame(intent EffectIntent, surface lifxdevice.Surface, width, height int) lifxeffects.Frame {
	effect := lifxeffects.NewGradient(lifxeffects.GradientConfig{
		Capabilities: effectCapabilities(surface, width, height),
		Palette:      effectPalette(intent.Palette, intent.Brightness),
	})
	frame, ok := effect.Next(time.Duration(intent.DurationMS) * time.Millisecond)
	if !ok {
		return frameFromPaletteColors(intent.Palette.GradientStops(width*height), width, height, intent.Brightness, intent.DurationMS)
	}
	return frame
}

func frameFromPaletteColors(colors []palette.Color, width, height int, brightness float64, durationMS int64) lifxeffects.Frame {
	frameColors := make([]lifxeffects.Color, len(colors))
	for i, color := range colors {
		frameColors[i] = effectColor(color, brightness)
	}
	return lifxeffects.Frame{
		Colors:   frameColors,
		Width:    width,
		Height:   height,
		Duration: time.Duration(durationMS) * time.Millisecond,
	}
}

func adaptFrame(frame lifxeffects.Frame, surface lifxdevice.Surface) []lifxeffects.DeviceFrame {
	frames, err := lifxeffects.AdaptFrameToSurface(frame, surface, lifxeffects.AdaptOptions{})
	if err != nil {
		return nil
	}
	return frames
}

func surfaceForMultiZone(device devices.DeviceInfo, count int) lifxdevice.Surface {
	surface := device.Capabilities.Surface
	surface.LightType = lifxdevice.LightTypeMultiZone
	if surface.Zones <= 0 {
		surface.Zones = count
	}
	if surface.Width <= 0 {
		surface.Width = surface.Zones
	}
	if surface.Height <= 0 {
		surface.Height = 1
	}
	return surface
}

func surfaceForMatrix(device devices.DeviceInfo, width, height int) lifxdevice.Surface {
	surface := device.Capabilities.Surface
	surface.LightType = lifxdevice.LightTypeMatrix
	if surface.Width <= 0 {
		surface.Width = width
	}
	if surface.Height <= 0 {
		surface.Height = height
	}
	if surface.Zones <= 0 {
		surface.Zones = width * height
	}
	if surface.Matrix == nil {
		surface.Matrix = &lifxdevice.MatrixSurface{Chains: []lifxdevice.MatrixChain{{
			Bounds:    lifxdevice.Rect{Width: width, Height: height},
			SendWidth: width,
		}}}
	}
	return surface
}

func effectCapabilities(surface lifxdevice.Surface, width, height int) lifxeffects.Capabilities {
	caps := lifxeffects.Capabilities{
		LightType: surface.LightType,
		Zones:     surface.Zones,
		Width:     surface.Width,
		Height:    surface.Height,
	}
	if caps.Width <= 0 {
		caps.Width = width
	}
	if caps.Height <= 0 {
		caps.Height = height
	}
	if caps.Zones <= 0 {
		caps.Zones = width * height
	}
	if surface.Matrix != nil {
		caps.ChainLength = len(surface.Matrix.Chains)
		caps.ChainOrientations = make([]lifxdevice.Orientation, 0, len(surface.Matrix.Chains))
		for _, chain := range surface.Matrix.Chains {
			caps.ChainOrientations = append(caps.ChainOrientations, chain.Orientation)
		}
	}
	return caps
}

func effectPalette(p palette.Palette, brightness float64) lifxeffects.Palette {
	return lifxeffects.Palette{
		Name:        p.Name,
		Base:        effectColors(p.Base, brightness),
		Accents:     effectColors(p.Accents, brightness),
		Backgrounds: effectColors(p.Backgrounds, brightness),
	}
}

func effectColors(colors []palette.Color, brightness float64) []lifxeffects.Color {
	converted := make([]lifxeffects.Color, len(colors))
	for i, color := range colors {
		converted[i] = effectColor(color, brightness)
	}
	return converted
}

func effectColor(color palette.Color, brightness float64) lifxeffects.Color {
	return lifxeffects.Color{
		Hue:        math.Round(color.Hue*10) / 10,
		Saturation: math.Round(clamp(color.Saturation, 0, 1)*1000) / 10,
		Brightness: math.Round(clamp(brightness, 0, 1)*1000) / 10,
		Kelvin:     uint16(color.Kelvin),
	}
}

func timelineColor(color lifxeffects.Color) timeline.ColorValue {
	return timeline.ColorValue{
		Hue:        math.Round(color.Hue*10) / 10,
		Saturation: math.Round(clamp(color.Saturation, 0, 100)*10) / 1000,
		Brightness: math.Round(clamp(color.Brightness, 0, 100)*10) / 1000,
		Kelvin:     int(color.Kelvin),
	}
}

func zoneColorParamsFromEffects(colors []lifxeffects.Color) []timeline.ZoneColorParams {
	zones := make([]timeline.ZoneColorParams, len(colors))
	for i, color := range colors {
		zones[i] = timeline.ZoneColorParams{Index: i, Color: timelineColor(color)}
	}
	return zones
}

func matrixColorParamsFromEffects(colors []lifxeffects.Color, width, height int) []timeline.MatrixColorParams {
	pixels := make([]timeline.MatrixColorParams, 0, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			if i >= len(colors) {
				continue
			}
			pixels = append(pixels, timeline.MatrixColorParams{X: x, Y: y, Color: timelineColor(colors[i])})
		}
	}
	return pixels
}

func durationMS(frame lifxeffects.DeviceFrame, fallback int64) int64 {
	if frame.Duration > 0 {
		return frame.Duration.Milliseconds()
	}
	return fallback
}

func zoneCount(device devices.DeviceInfo) int {
	if device.Capabilities.ZoneCount > 0 {
		return device.Capabilities.ZoneCount
	}
	if device.Capabilities.Surface.Zones > 0 {
		return device.Capabilities.Surface.Zones
	}
	return 1
}

func matrixDimensions(device devices.DeviceInfo) (int, int) {
	width := device.Capabilities.MatrixWidth
	height := device.Capabilities.MatrixHeight
	if width <= 0 {
		width = device.Capabilities.Surface.Width
	}
	if height <= 0 {
		height = device.Capabilities.Surface.Height
	}
	if width <= 0 {
		width = 8
	}
	if height <= 0 {
		height = 8
	}
	return width, height
}
