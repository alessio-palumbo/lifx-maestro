package generation

import (
	"fmt"
	"strings"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/effects"
	"lifx-maestro/internal/rendering"
	"lifx-maestro/internal/sections"
	"lifx-maestro/internal/styles"
	"lifx-maestro/internal/timeline"
)

type Config struct {
	Style                    string             `json:"style"`
	BrightnessScale          float64            `json:"brightness_scale"`
	TransitionAggressiveness float64            `json:"transition_aggressiveness"`
	Mode                     GenerationMode     `json:"mode,omitempty"`
	Dynamics                 DynamicsOverride   `json:"dynamics,omitempty"`
	Assignments              []StreamAssignment `json:"assignments,omitempty"`
}

type GenerationMode string

const (
	GenerationModeSongWide      GenerationMode = "song_wide"
	GenerationModeMusicalLayers GenerationMode = "musical_layers"
)

type DynamicsOverride string

const (
	DynamicsAuto      DynamicsOverride = "auto"
	DynamicsCalm      DynamicsOverride = "calm"
	DynamicsBalanced  DynamicsOverride = "balanced"
	DynamicsEnergetic DynamicsOverride = "energetic"
)

type StreamAssignment struct {
	Stream    string   `json:"stream"`
	DeviceIDs []string `json:"device_ids"`
}

type Options struct {
	Name        string
	Target      string
	Style       string
	Mode        GenerationMode
	Dynamics    DynamicsOverride
	Assignments []StreamAssignment
	Config      Config
	Devices     []devices.DeviceInfo
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
	if err := ValidateMode(generationMode(options)); err != nil {
		return nil, err
	}
	dynamicsOverride := generationDynamics(options)
	if err := ValidateDynamics(dynamicsOverride); err != nil {
		return nil, err
	}
	dynamics := dynamicsFor(song.Dynamics, dynamicsOverride)

	targets := targetsFor(options.Target, options.Devices)
	songSections := sections.FromAnalysis(song)

	tl := &timeline.Timeline{
		Name:       options.Name,
		DurationMS: song.DurationMS,
		Events: []timeline.Event{
			{TimeMS: 0, Target: options.Target, Action: "power_on"},
		},
	}

	if generationMode(options) == GenerationModeMusicalLayers {
		tl.Events = append(tl.Events, layeredEvents(song, style, targets, songSections, generationAssignments(options), dynamics)...)
	} else {
		for i, section := range songSections {
			if i > 0 {
				tl.Events = append(tl.Events, transitionEvents(song, section, style, targets, i, dynamics)...)
			}
			tl.Events = append(tl.Events, sectionEvents(song, section, style, targets, i, dynamics)...)
		}
	}

	if len(tl.Events) == 1 {
		tl.Events = append(tl.Events, fallbackEvents(song, style, targets, dynamics)...)
	}

	tl.SortEvents()
	tl.Events = normalizeTimelineEvents(tl.Events)
	return tl, nil
}

func layeredEvents(song analysis.SongAnalysis, style styles.Style, targets []effects.Target, songSections []sections.Section, assignments []StreamAssignment, dynamics dynamicsPolicy) []timeline.Event {
	if len(song.Streams) == 0 {
		var events []timeline.Event
		for i, section := range songSections {
			if i > 0 {
				events = append(events, transitionEvents(song, section, style, targets, i, dynamics)...)
			}
			events = append(events, sectionEvents(song, section, style, targets, i, dynamics)...)
		}
		return events
	}

	streamTargets := assignedStreamTargets(targets, assignments)
	var events []timeline.Event
	for i, section := range songSections {
		if i > 0 {
			events = append(events, transitionEvents(song, section, style, targetGroup(streamTargets, "full", targets), i, dynamics)...)
		}
		events = append(events, streamSectionEvents(song, section, style, streamTargets, i, dynamics)...)
	}
	return events
}

func streamSectionEvents(song analysis.SongAnalysis, section sections.Section, style styles.Style, streamTargets map[string][]effects.Target, sectionIndex int, dynamics dynamicsPolicy) []timeline.Event {
	var events []timeline.Event
	streams := streamsByID(song.Streams)
	for _, spec := range []struct {
		id       string
		section  sections.Type
		effect   string
		beatStep int
		duration float64
		minScale float64
		maxScale float64
		shift    int
		minGapMS int64
	}{
		{id: "low", section: sections.TypeDrop, effect: "sweep", beatStep: 2, duration: 1.45, minScale: 0.9, maxScale: 1.05, minGapMS: 260},
		{id: "high", section: sections.TypeDrop, effect: "sweep", beatStep: 1, duration: 0.45, minScale: 0.75, maxScale: 1.18, shift: 1, minGapMS: 150},
		{id: "mid", section: sections.TypeBreakdown, effect: "breathing", beatStep: 4, duration: 2.2, minScale: 0.85, maxScale: 0.78, shift: 2, minGapMS: 360},
	} {
		stream, ok := streams[spec.id]
		targets := streamTargets[spec.id]
		if !ok || len(targets) == 0 {
			continue
		}
		streamSection := section
		streamSection.Type = spec.section
		streamSection.Energy = meanEnergy(stream.Energy, section.StartMS, section.EndMS, section.Energy)
		ctx := effects.Context{
			Section:     streamSection,
			Beats:       thinTimes(streamBeatsInSection(stream.Accents, song.Beats, section), spec.minGapMS),
			Energy:      stream.Energy,
			Targets:     targets,
			Palette:     style.Palette,
			MinBright:   minBrightness(section, style) * spec.minScale * dynamics.brightnessScale,
			MaxBright:   maxBrightness(section, style) * spec.maxScale * dynamics.brightnessScale,
			DurationMS:  max(45, int64(float64(effectDuration(song.BPM, streamSection, style))*spec.duration*dynamics.durationScale)),
			BeatStep:    max(spec.beatStep, dynamics.minBeatStep),
			TargetShift: sectionIndex + spec.shift,
		}
		switch spec.effect {
		case "breathing":
			events = append(events, effects.Breathing{}.Generate(ctx)...)
			ctx.DurationMS = max(90, ctx.DurationMS/4)
			events = append(events, effects.Pulse{}.Generate(ctx)...)
		case "sweep":
			events = append(events, effects.Sweep{}.Generate(ctx)...)
		default:
			events = append(events, effects.Pulse{}.Generate(ctx)...)
		}
	}

	if full, ok := streams["full"]; ok {
		targets := targetGroup(streamTargets, "full", nil)
		if len(targets) > 0 {
			ctx := effects.Context{
				Section:     section,
				Beats:       majorAccentsInSection(thinTimes(full.Accents, 300), section),
				Energy:      full.Energy,
				Targets:     targets,
				Palette:     style.Palette,
				MinBright:   minBrightness(section, style) * dynamics.brightnessScale,
				MaxBright:   clamp(maxBrightness(section, style)*1.08*dynamics.brightnessScale, 0.08, 1),
				DurationMS:  max(45, int64(float64(effectDuration(song.BPM, section, style))*dynamics.durationScale/2)),
				BeatStep:    dynamics.minBeatStep,
				TargetShift: sectionIndex,
			}
			events = append(events, effects.Pulse{}.Generate(ctx)...)
		}
	}
	return events
}

func sectionEvents(song analysis.SongAnalysis, section sections.Section, style styles.Style, targets []effects.Target, sectionIndex int, dynamics dynamicsPolicy) []timeline.Event {
	ctx := effects.Context{
		Section:     section,
		Beats:       song.Beats,
		Energy:      song.Energy,
		Targets:     targets,
		Palette:     style.Palette,
		MinBright:   minBrightness(section, style) * dynamics.brightnessScale,
		MaxBright:   maxBrightness(section, style) * dynamics.brightnessScale,
		DurationMS:  int64(float64(effectDuration(song.BPM, section, style)) * dynamics.durationScale),
		BeatStep:    max(beatStep(section, style), dynamics.minBeatStep),
		TargetShift: sectionIndex,
	}

	switch section.Type {
	case sections.TypeIntro:
		events := effects.Breathing{}.Generate(ctx)
		ctx.BeatStep = max(4, style.PulseEvery)
		ctx.DurationMS = max(90, ctx.DurationMS/3)
		ctx.MaxBright *= 0.78
		return append(events, effects.Pulse{}.Generate(ctx)...)
	case sections.TypeBuild:
		return effects.AlternatingPulse{}.Generate(ctx)
	case sections.TypeDrop:
		if dynamics.profile == dynamicsCalm {
			events := effects.Breathing{}.Generate(ctx)
			ctx.DurationMS = max(120, ctx.DurationMS/3)
			return append(events, effects.Pulse{}.Generate(ctx)...)
		}
		ctx.DurationMS = max(45, ctx.DurationMS/2)
		return effects.Sweep{}.Generate(ctx)
	case sections.TypeBreakdown:
		events := effects.Breathing{}.Generate(ctx)
		ctx.BeatStep = max(2, style.PulseEvery)
		ctx.DurationMS = max(120, ctx.DurationMS/4)
		ctx.MaxBright *= 0.82
		return append(events, effects.Pulse{}.Generate(ctx)...)
	case sections.TypeOutro:
		events := effects.Breathing{}.Generate(ctx)
		ctx.MaxBright *= 0.65
		ctx.BeatStep = max(4, style.PulseEvery)
		ctx.DurationMS = max(120, ctx.DurationMS/3)
		return append(events, effects.Pulse{}.Generate(ctx)...)
	default:
		return effects.Pulse{}.Generate(ctx)
	}
}

func fallbackEvents(song analysis.SongAnalysis, style styles.Style, targets []effects.Target, dynamics dynamicsPolicy) []timeline.Event {
	section := sections.Section{StartMS: 0, EndMS: song.DurationMS, Type: sections.TypeDrop, Energy: 0.5}
	return sectionEvents(song, section, style, targets, 0, dynamics)
}

func transitionEvents(song analysis.SongAnalysis, section sections.Section, style styles.Style, targets []effects.Target, sectionIndex int, dynamics dynamicsPolicy) []timeline.Event {
	if len(targets) == 0 {
		return nil
	}

	beatMS := beatDurationMS(song.BPM)
	pulses := transitionPulseCount(section.Type)
	if dynamics.maxTransitionPulses > 0 {
		pulses = min(pulses, dynamics.maxTransitionPulses)
	}
	if pulses == 0 {
		return nil
	}
	durationMS := max(40, int64(float64(beatMS/4)*dynamics.durationScale))
	brightness := clamp(maxBrightness(section, style)*1.15*dynamics.brightnessScale, 0.18, 1.0)
	kind := rendering.IntentPulse

	switch section.Type {
	case sections.TypeBuild:
		kind = rendering.IntentPulse
	case sections.TypeDrop:
		if dynamics.profile != dynamicsCalm {
			kind = rendering.IntentSweep
		}
	case sections.TypeBreakdown:
		brightness *= 0.75
	case sections.TypeOutro:
		brightness *= 0.62
	}

	var events []timeline.Event
	for pulse := 0; pulse < pulses; pulse++ {
		timeMS := section.StartMS + beatMS/2 + int64(pulse)*beatMS
		if timeMS >= section.EndMS {
			break
		}
		for targetOffset, target := range targets {
			beatIndex := sectionIndex + pulse + targetOffset
			events = append(events, rendering.Render(rendering.EffectIntent{
				Kind:        kind,
				TimeMS:      timeMS,
				Target:      target.DeviceID,
				Color:       style.Palette.AccentForBeat(beatIndex),
				Palette:     style.Palette,
				Brightness:  brightness,
				DurationMS:  durationMS,
				BeatIndex:   beatIndex,
				Section:     string(section.Type),
				DeviceIndex: target.Index,
				DeviceTotal: target.Total,
				Supported:   rendering.SupportedDeviceKinds{SingleZone: true, MultiZone: true, Matrix: true},
			}, devices.DeviceInfo{ID: target.DeviceID, Capabilities: target.Capabilities})...)
		}
	}
	return events
}

func transitionPulseCount(sectionType sections.Type) int {
	switch sectionType {
	case sections.TypeBuild:
		return 4
	case sections.TypeDrop:
		return 8
	case sections.TypeBreakdown, sections.TypeOutro:
		return 3
	default:
		return 0
	}
}

func beatDurationMS(bpm float64) int64 {
	beatMS := int64(500)
	if bpm > 0 {
		beatMS = int64(60000 / bpm)
	}
	return min(max(beatMS, 220), 1200)
}

type dynamicsProfile string

const (
	dynamicsCalm      dynamicsProfile = "calm"
	dynamicsBalanced  dynamicsProfile = "balanced"
	dynamicsEnergetic dynamicsProfile = "energetic"
)

type dynamicsPolicy struct {
	profile             dynamicsProfile
	brightnessScale     float64
	durationScale       float64
	minBeatStep         int
	maxTransitionPulses int
}

func dynamicsFor(dynamics analysis.TrackDynamics, override DynamicsOverride) dynamicsPolicy {
	profile := dynamics.Profile
	if override != "" && override != DynamicsAuto {
		profile = string(override)
	}
	switch profile {
	case string(dynamicsCalm):
		return dynamicsPolicy{
			profile:             dynamicsCalm,
			brightnessScale:     0.78,
			durationScale:       1.8,
			minBeatStep:         4,
			maxTransitionPulses: 2,
		}
	case string(dynamicsBalanced):
		return dynamicsPolicy{
			profile:             dynamicsBalanced,
			brightnessScale:     0.9,
			durationScale:       1.3,
			minBeatStep:         2,
			maxTransitionPulses: 4,
		}
	default:
		// Analyses cached before track dynamics were introduced have an empty
		// profile. Preserve their previous generation behavior.
		return dynamicsPolicy{
			profile:         dynamicsEnergetic,
			brightnessScale: 1,
			durationScale:   1,
			minBeatStep:     1,
		}
	}
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

func generationMode(options Options) GenerationMode {
	if options.Config.Mode != "" {
		return options.Config.Mode
	}
	if options.Mode != "" {
		return options.Mode
	}
	return GenerationModeSongWide
}

func generationAssignments(options Options) []StreamAssignment {
	if len(options.Config.Assignments) > 0 {
		return options.Config.Assignments
	}
	return options.Assignments
}

func generationDynamics(options Options) DynamicsOverride {
	if options.Config.Dynamics != "" {
		return options.Config.Dynamics
	}
	if options.Dynamics != "" {
		return options.Dynamics
	}
	return DynamicsAuto
}

func streamsByID(streams []analysis.Stream) map[string]analysis.Stream {
	out := make(map[string]analysis.Stream, len(streams))
	for _, stream := range streams {
		out[stream.ID] = stream
	}
	return out
}

func assignedStreamTargets(targets []effects.Target, assignments []StreamAssignment) map[string][]effects.Target {
	out := make(map[string][]effects.Target)
	if len(assignments) > 0 {
		byID := make(map[string]effects.Target, len(targets))
		for _, target := range targets {
			byID[strings.ToLower(target.DeviceID)] = target
		}
		for _, assignment := range assignments {
			stream := strings.ToLower(strings.TrimSpace(assignment.Stream))
			if stream == "" {
				continue
			}
			for _, id := range assignment.DeviceIDs {
				target, ok := byID[strings.ToLower(strings.TrimSpace(id))]
				if ok {
					out[stream] = append(out[stream], target)
					if stream != "full" {
						out["full"] = append(out["full"], target)
					}
				}
			}
		}
		for stream, assigned := range out {
			out[stream] = assignTargetIndexes(uniqueTargets(assigned))
		}
		return out
	}

	for _, target := range targets {
		switch target.Capabilities.Kind {
		case devices.DeviceKindMatrix:
			out["high"] = append(out["high"], target)
			out["full"] = append(out["full"], target)
		case devices.DeviceKindMultiZone:
			out["low"] = append(out["low"], target)
			out["full"] = append(out["full"], target)
		case devices.DeviceKindSingleZone:
			out["mid"] = append(out["mid"], target)
			out["full"] = append(out["full"], target)
		}
	}
	for stream, assigned := range out {
		out[stream] = assignTargetIndexes(uniqueTargets(assigned))
	}
	return out
}

func uniqueTargets(targets []effects.Target) []effects.Target {
	seen := make(map[string]bool)
	out := make([]effects.Target, 0, len(targets))
	for _, target := range targets {
		if target.DeviceID == "" || seen[target.DeviceID] {
			continue
		}
		seen[target.DeviceID] = true
		out = append(out, target)
	}
	return out
}

func targetGroup(groups map[string][]effects.Target, stream string, fallback []effects.Target) []effects.Target {
	targets := groups[stream]
	if len(targets) == 0 {
		return fallback
	}
	return targets
}

func streamBeatsInSection(accents, fallbackBeats []int64, section sections.Section) []int64 {
	beats := timesInSection(accents, section)
	if len(beats) > 0 {
		return beats
	}
	return timesInSection(fallbackBeats, section)
}

func majorAccentsInSection(accents []int64, section sections.Section) []int64 {
	inSection := timesInSection(accents, section)
	if len(inSection) <= 8 {
		return inSection
	}
	step := max(1, len(inSection)/8)
	var out []int64
	for i, accent := range inSection {
		if i%step == 0 {
			out = append(out, accent)
		}
	}
	return out
}

func timesInSection(times []int64, section sections.Section) []int64 {
	var out []int64
	for _, timeMS := range times {
		if timeMS >= section.StartMS && timeMS < section.EndMS {
			out = append(out, timeMS)
		}
	}
	return out
}

func thinTimes(times []int64, minGapMS int64) []int64 {
	if minGapMS <= 0 || len(times) < 2 {
		return times
	}
	out := []int64{times[0]}
	last := times[0]
	for _, timeMS := range times[1:] {
		if timeMS-last >= minGapMS {
			out = append(out, timeMS)
			last = timeMS
		}
	}
	return out
}

func meanEnergy(points []analysis.EnergyPoint, startMS, endMS int64, fallback float64) float64 {
	var total float64
	var count int
	for _, point := range points {
		if point.TimeMS >= startMS && point.TimeMS < endMS {
			total += point.Value
			count++
		}
	}
	if count == 0 {
		return fallback
	}
	return total / float64(count)
}

func targetsFor(target string, infos []devices.DeviceInfo) []effects.Target {
	names := splitTargets(target)
	if len(infos) > 0 {
		targets := targetsFromDeviceInfos(names, infos)
		if len(targets) > 0 {
			return targets
		}
	}

	if len(names) > 1 {
		targets := make([]effects.Target, 0, len(names))
		for _, name := range names {
			targets = append(targets, effects.Target{DeviceID: name, Capabilities: defaultCapabilities()})
		}
		return assignTargetIndexes(targets)
	}

	return assignTargetIndexes([]effects.Target{{DeviceID: target, Capabilities: defaultCapabilities()}})
}

func targetsFromDeviceInfos(names []string, infos []devices.DeviceInfo) []effects.Target {
	seen := make(map[string]bool)
	var targets []effects.Target
	add := func(info devices.DeviceInfo) {
		if info.ID == "" || seen[info.ID] {
			return
		}
		seen[info.ID] = true
		targets = append(targets, effects.Target{DeviceID: info.ID, Capabilities: info.Capabilities})
	}

	for _, name := range names {
		if name == "all" {
			for _, info := range infos {
				add(info)
			}
			continue
		}
		for _, info := range infos {
			if matchesDeviceInfo(name, info) {
				add(info)
			}
		}
	}
	return assignTargetIndexes(targets)
}

func matchesDeviceInfo(selector string, info devices.DeviceInfo) bool {
	selector = strings.ToLower(strings.TrimSpace(selector))
	return selector != "" && (strings.ToLower(info.ID) == selector ||
		strings.ToLower(info.Label) == selector ||
		strings.ToLower(info.Group) == selector ||
		strings.ToLower(info.Location) == selector)
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

func assignTargetIndexes(targets []effects.Target) []effects.Target {
	for i := range targets {
		targets[i].Index = i
		targets[i].Total = len(targets)
	}
	return targets
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

	return max(45, int64(duration))
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

func AvailableStyles() []string {
	return styles.Names()
}

func AvailableModes() []string {
	return []string{string(GenerationModeSongWide), string(GenerationModeMusicalLayers)}
}

func AvailableDynamics() []string {
	return []string{string(DynamicsAuto), string(DynamicsCalm), string(DynamicsBalanced), string(DynamicsEnergetic)}
}

func ValidateStyle(name string) error {
	_, err := styles.Get(name)
	if err != nil {
		return fmt.Errorf("%w; available styles: %s", err, strings.Join(AvailableStyles(), ", "))
	}
	return nil
}

func ValidateMode(mode GenerationMode) error {
	switch mode {
	case "", GenerationModeSongWide, GenerationModeMusicalLayers:
		return nil
	default:
		return fmt.Errorf("unsupported generation mode %q; available modes: %s", mode, strings.Join(AvailableModes(), ", "))
	}
}

func ValidateDynamics(dynamics DynamicsOverride) error {
	switch dynamics {
	case "", DynamicsAuto, DynamicsCalm, DynamicsBalanced, DynamicsEnergetic:
		return nil
	default:
		return fmt.Errorf("unsupported dynamics override %q; available values: %s", dynamics, strings.Join(AvailableDynamics(), ", "))
	}
}
