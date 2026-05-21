package generation

import (
	"fmt"
	"math"
	"strings"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/timeline"
)

type Mode string

const (
	ModeDefault   Mode = "default"
	ModeAmbient   Mode = "ambient"
	ModeEnergetic Mode = "energetic"
)

type Options struct {
	Name   string
	Target string
	Mode   Mode
}

type modeConfig struct {
	palette       []float64
	minBrightness float64
	maxBrightness float64
	minDurationMS int64
	maxDurationMS int64
	saturation    float64
}

func Generate(song analysis.SongAnalysis, options Options) (*timeline.Timeline, error) {
	if err := song.Validate(); err != nil {
		return nil, err
	}
	if options.Name == "" {
		options.Name = "generated"
	}
	if options.Target == "" {
		options.Target = "all"
	}
	if options.Mode == "" {
		options.Mode = ModeDefault
	}

	cfg, err := configForMode(options.Mode)
	if err != nil {
		return nil, err
	}

	tl := &timeline.Timeline{
		Name:       options.Name,
		DurationMS: song.DurationMS,
		Events: []timeline.Event{
			{TimeMS: 0, Target: options.Target, Action: "power_on"},
		},
	}

	if len(song.Beats) == 0 {
		tl.Events = append(tl.Events, energyEvents(song, options.Target, cfg)...)
	} else {
		for i, beat := range song.Beats {
			if beat > song.DurationMS {
				continue
			}
			eventTime := beat
			if eventTime == 0 {
				eventTime = 1
			}
			energy := energyAt(song.Energy, beat)
			hue := cfg.palette[i%len(cfg.palette)]
			tl.Events = append(tl.Events, colorEvent(eventTime, options.Target, hue, cfg.saturation, brightnessForEnergy(energy, cfg), durationForEnergy(energy, cfg)))
		}
	}

	tl.SortEvents()
	return tl, nil
}

func energyEvents(song analysis.SongAnalysis, target string, cfg modeConfig) []timeline.Event {
	events := make([]timeline.Event, 0, len(song.Energy))
	for i, point := range song.Energy {
		if point.TimeMS > song.DurationMS {
			continue
		}
		eventTime := point.TimeMS
		if eventTime == 0 {
			eventTime = 1
		}
		hue := cfg.palette[i%len(cfg.palette)]
		events = append(events, colorEvent(eventTime, target, hue, cfg.saturation, brightnessForEnergy(point.Value, cfg), durationForEnergy(point.Value, cfg)))
	}
	return events
}

func colorEvent(timeMS int64, target string, hue, saturation, brightness float64, durationMS int64) timeline.Event {
	return timeline.Event{
		TimeMS: timeMS,
		Target: target,
		Action: "set_color",
		Params: timeline.MustParams(timeline.SetColorParams{
			Hue:        ptr(math.Round(hue*10) / 10),
			Saturation: ptr(math.Round(saturation*1000) / 1000),
			Brightness: ptr(math.Round(brightness*1000) / 1000),
			Kelvin:     ptr(3500),
			DurationMS: ptr(durationMS),
		}),
	}
}

func ptr[T any](value T) *T {
	return &value
}

func energyAt(points []analysis.EnergyPoint, timeMS int64) float64 {
	if len(points) == 0 {
		return 0.5
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

func brightnessForEnergy(energy float64, cfg modeConfig) float64 {
	energy = clamp(energy, 0, 1)
	return cfg.minBrightness + (cfg.maxBrightness-cfg.minBrightness)*energy
}

func durationForEnergy(energy float64, cfg modeConfig) int64 {
	energy = clamp(energy, 0, 1)
	duration := float64(cfg.maxDurationMS) - (float64(cfg.maxDurationMS-cfg.minDurationMS) * energy)
	return int64(math.Round(duration))
}

func configForMode(mode Mode) (modeConfig, error) {
	switch Mode(strings.ToLower(string(mode))) {
	case ModeDefault:
		return modeConfig{
			palette:       []float64{220, 275},
			minBrightness: 0.35,
			maxBrightness: 0.95,
			minDurationMS: 90,
			maxDurationMS: 280,
			saturation:    1.0,
		}, nil
	case ModeAmbient:
		return modeConfig{
			palette:       []float64{200, 260},
			minBrightness: 0.18,
			maxBrightness: 0.65,
			minDurationMS: 250,
			maxDurationMS: 700,
			saturation:    0.75,
		}, nil
	case ModeEnergetic:
		return modeConfig{
			palette:       []float64{80, 150, 180, 210, 285, 330, 360},
			minBrightness: 0.55,
			maxBrightness: 1.0,
			minDurationMS: 45,
			maxDurationMS: 160,
			saturation:    1.0,
		}, nil
	default:
		return modeConfig{}, fmt.Errorf("unsupported generation mode %q", mode)
	}
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
