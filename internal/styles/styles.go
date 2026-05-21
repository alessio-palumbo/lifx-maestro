package styles

import (
	"fmt"
	"sort"

	"lifx-maestro/internal/palette"
)

type Style struct {
	Name                     string
	Palette                  palette.Palette
	BrightnessScale          float64
	TransitionAggressiveness float64
	PulseEvery               int
	OffsetMS                 int64
}

func Get(name string) (Style, error) {
	all := All()
	style, ok := all[name]
	if !ok {
		return Style{}, fmt.Errorf("unsupported style %q", name)
	}
	return style, nil
}

func All() map[string]Style {
	palettes := palette.All()
	return map[string]Style{
		"synthwave": {
			Name: "synthwave", Palette: palettes["synthwave"],
			BrightnessScale: 0.92, TransitionAggressiveness: 0.72, PulseEvery: 1, OffsetMS: 55,
		},
		"cinematic": {
			Name: "cinematic", Palette: palettes["cinematic"],
			BrightnessScale: 0.78, TransitionAggressiveness: 0.38, PulseEvery: 2, OffsetMS: 120,
		},
		"warm": {
			Name: "warm", Palette: palettes["warm"],
			BrightnessScale: 0.72, TransitionAggressiveness: 0.28, PulseEvery: 2, OffsetMS: 90,
		},
		"neon": {
			Name: "neon", Palette: palettes["neon"],
			BrightnessScale: 0.96, TransitionAggressiveness: 0.82, PulseEvery: 1, OffsetMS: 45,
		},
		"minimal": {
			Name: "minimal", Palette: palettes["minimal"],
			BrightnessScale: 0.58, TransitionAggressiveness: 0.18, PulseEvery: 4, OffsetMS: 160,
		},
		"cyberpunk": {
			Name: "cyberpunk", Palette: palettes["cyberpunk"],
			BrightnessScale: 1.0, TransitionAggressiveness: 0.88, PulseEvery: 1, OffsetMS: 35,
		},
	}
}

func Names() []string {
	names := make([]string, 0, len(All()))
	for name := range All() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
