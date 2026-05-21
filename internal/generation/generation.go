package generation

import (
	"fmt"
	"strings"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/effects"
	"lifx-maestro/internal/sections"
	"lifx-maestro/internal/styles"
	"lifx-maestro/internal/timeline"
)

type Config struct {
	Style                    string  `json:"style"`
	BrightnessScale          float64 `json:"brightness_scale"`
	TransitionAggressiveness float64 `json:"transition_aggressiveness"`
}

type Options struct {
	Name    string
	Target  string
	Style   string
	Config  Config
	Devices []devices.DeviceInfo
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

	styleName := styleName(options)
	style, err := styles.Get(styleName)
	if err != nil {
		return nil, err
	}
	style = applyConfig(style, options.Config)

	targets := targetsFor(options.Target, options.Devices)
	songSections := sections.FromAnalysis(song)

	tl := &timeline.Timeline{
		Name:       options.Name,
		DurationMS: song.DurationMS,
		Events: []timeline.Event{
			{TimeMS: 0, Target: options.Target, Action: "power_on"},
		},
	}

	for i, section := range songSections {
		tl.Events = append(tl.Events, sectionEvents(song, section, style, targets, i)...)
	}

	if len(tl.Events) == 1 {
		tl.Events = append(tl.Events, fallbackEvents(song, style, targets)...)
	}

	tl.SortEvents()
	return tl, nil
}

func sectionEvents(song analysis.SongAnalysis, section sections.Section, style styles.Style, targets []effects.Target, sectionIndex int) []timeline.Event {
	ctx := effects.Context{
		Section:     section,
		Beats:       song.Beats,
		Energy:      song.Energy,
		Targets:     targets,
		Palette:     style.Palette,
		MinBright:   minBrightness(section, style),
		MaxBright:   maxBrightness(section, style),
		DurationMS:  effectDuration(song.BPM, section, style),
		BeatStep:    beatStep(section, style),
		TargetShift: sectionIndex,
	}

	switch section.Type {
	case sections.TypeIntro:
		return effects.Breathing{}.Generate(ctx)
	case sections.TypeBuild:
		return effects.AlternatingPulse{}.Generate(ctx)
	case sections.TypeDrop:
		ctx.BeatStep = 1
		ctx.DurationMS = maxInt64(45, ctx.DurationMS/2)
		return effects.Sweep{}.Generate(ctx)
	case sections.TypeBreakdown:
		return effects.Fade{}.Generate(ctx)
	case sections.TypeOutro:
		ctx.MaxBright *= 0.65
		return effects.Breathing{}.Generate(ctx)
	default:
		return effects.Pulse{}.Generate(ctx)
	}
}

func fallbackEvents(song analysis.SongAnalysis, style styles.Style, targets []effects.Target) []timeline.Event {
	section := sections.Section{StartMS: 0, EndMS: song.DurationMS, Type: sections.TypeDrop, Energy: 0.5}
	return sectionEvents(song, section, style, targets, 0)
}

func styleName(options Options) string {
	if options.Config.Style != "" {
		return options.Config.Style
	}
	if options.Style != "" {
		return options.Style
	}
	return "synthwave"
}

func applyConfig(style styles.Style, config Config) styles.Style {
	if config.BrightnessScale > 0 {
		style.BrightnessScale = config.BrightnessScale
	}
	if config.TransitionAggressiveness > 0 {
		style.TransitionAggressiveness = config.TransitionAggressiveness
	}
	return style
}

func targetsFor(target string, infos []devices.DeviceInfo) []effects.Target {
	names := splitTargets(target)
	if len(names) > 1 {
		targets := make([]effects.Target, 0, len(names))
		for _, name := range names {
			targets = append(targets, effects.Target{DeviceID: name, Capabilities: defaultCapabilities()})
		}
		return targets
	}

	if target == "all" && len(infos) > 0 {
		targets := make([]effects.Target, 0, len(infos))
		for _, info := range infos {
			if info.ID == "" {
				continue
			}
			targets = append(targets, effects.Target{DeviceID: info.ID, Capabilities: info.Capabilities})
		}
		if len(targets) > 0 {
			return targets
		}
	}

	return []effects.Target{{DeviceID: target, Capabilities: defaultCapabilities()}}
}

func splitTargets(target string) []string {
	parts := strings.Split(target, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func defaultCapabilities() devices.DeviceCapabilities {
	return devices.DeviceCapabilities{
		Kind:      devices.DeviceKindSingleZone,
		HasColor:  true,
		HasKelvin: true,
		ZoneCount: 1,
	}
}

func minBrightness(section sections.Section, style styles.Style) float64 {
	switch section.Type {
	case sections.TypeIntro, sections.TypeOutro:
		return 0.12 * style.BrightnessScale
	case sections.TypeBreakdown:
		return 0.1 * style.BrightnessScale
	default:
		return 0.24 * style.BrightnessScale
	}
}

func maxBrightness(section sections.Section, style styles.Style) float64 {
	base := 0.45 + section.Energy*0.5
	switch section.Type {
	case sections.TypeDrop:
		base += 0.16
	case sections.TypeBreakdown:
		base *= 0.62
	}
	return clamp(base*style.BrightnessScale, 0.08, 1.0)
}

func beatStep(section sections.Section, style styles.Style) int {
	if section.Type == sections.TypeDrop {
		return 1
	}
	if style.PulseEvery <= 0 {
		return 1
	}
	return style.PulseEvery
}

func effectDuration(bpm float64, section sections.Section, style styles.Style) int64 {
	beatMS := 500.0
	if bpm > 0 {
		beatMS = 60000 / bpm
	}
	aggression := clamp(style.TransitionAggressiveness, 0, 1)
	duration := beatMS * (0.85 - aggression*0.55)

	switch section.Type {
	case sections.TypeIntro, sections.TypeOutro:
		duration *= 2.8
	case sections.TypeBuild:
		duration *= 1.2
	case sections.TypeDrop:
		duration *= 0.65
	case sections.TypeBreakdown:
		duration *= 4.0
	}

	return maxInt64(45, int64(duration))
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func AvailableStyles() []string {
	return styles.Names()
}

func ValidateStyle(name string) error {
	_, err := styles.Get(name)
	if err != nil {
		return fmt.Errorf("%w; available styles: %s", err, strings.Join(AvailableStyles(), ", "))
	}
	return nil
}
