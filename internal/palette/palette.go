package palette

type Color struct {
	Hue        float64
	Saturation float64
	Kelvin     int
}

type Palette struct {
	Name        string
	Base        []Color
	Accents     []Color
	Backgrounds []Color
}

func (p Palette) Primary() Color {
	return firstOr(p.Base, Color{Hue: 220, Saturation: 1, Kelvin: 3500})
}

func (p Palette) Secondary() Color {
	if len(p.Base) > 1 {
		return p.Base[1]
	}
	return p.Primary()
}

func (p Palette) Accent() Color {
	return firstOr(p.Accents, p.Primary())
}

func (p Palette) ColorForDevice(index, total int) Color {
	colors := append([]Color{}, p.Base...)
	colors = append(colors, p.Accents...)
	if len(colors) == 0 {
		return p.Primary()
	}
	if total <= 0 {
		total = len(colors)
	}
	return colors[index%len(colors)]
}

func (p Palette) AccentForBeat(beatIndex int) Color {
	if len(p.Accents) == 0 {
		return p.ColorForDevice(beatIndex, len(p.Base))
	}
	return p.Accents[beatIndex%len(p.Accents)]
}

func (p Palette) GradientStops(count int) []Color {
	if count <= 0 {
		return nil
	}
	source := append([]Color{}, p.Backgrounds...)
	source = append(source, p.Base...)
	source = append(source, p.Accents...)
	if len(source) == 0 {
		source = []Color{p.Primary()}
	}
	stops := make([]Color, count)
	for i := range stops {
		stops[i] = source[i%len(source)]
	}
	return stops
}

func (p Palette) BackgroundForSection(sectionType string) Color {
	if len(p.Backgrounds) == 0 {
		return p.Secondary()
	}
	switch sectionType {
	case "drop":
		return p.Backgrounds[len(p.Backgrounds)-1]
	case "breakdown", "outro":
		return p.Backgrounds[0]
	default:
		return p.Backgrounds[len(p.Backgrounds)/2]
	}
}

func All() map[string]Palette {
	return map[string]Palette{
		"synthwave": {
			Name: "synthwave",
			Base: []Color{
				{Hue: 285, Saturation: 1.0, Kelvin: 3500},
				{Hue: 200, Saturation: 1.0, Kelvin: 3500},
				{Hue: 255, Saturation: 0.92, Kelvin: 3600},
				{Hue: 315, Saturation: 0.95, Kelvin: 3400},
			},
			Accents: []Color{
				{Hue: 330, Saturation: 1.0, Kelvin: 3500},
				{Hue: 180, Saturation: 1.0, Kelvin: 4200},
				{Hue: 45, Saturation: 0.95, Kelvin: 3000},
			},
			Backgrounds: []Color{
				{Hue: 235, Saturation: 0.55, Kelvin: 3600},
				{Hue: 275, Saturation: 0.65, Kelvin: 3400},
			},
		},
		"cinematic": {
			Name: "cinematic",
			Base: []Color{
				{Hue: 220, Saturation: 0.72, Kelvin: 3200},
				{Hue: 38, Saturation: 0.65, Kelvin: 2700},
				{Hue: 250, Saturation: 0.42, Kelvin: 4200},
				{Hue: 18, Saturation: 0.58, Kelvin: 2600},
			},
			Accents: []Color{
				{Hue: 12, Saturation: 0.82, Kelvin: 3000},
				{Hue: 205, Saturation: 0.86, Kelvin: 4700},
			},
			Backgrounds: []Color{
				{Hue: 225, Saturation: 0.35, Kelvin: 3600},
				{Hue: 30, Saturation: 0.28, Kelvin: 2400},
			},
		},
		"warm": {
			Name: "warm",
			Base: []Color{
				{Hue: 28, Saturation: 0.55, Kelvin: 2600},
				{Hue: 48, Saturation: 0.48, Kelvin: 3000},
				{Hue: 12, Saturation: 0.5, Kelvin: 2400},
				{Hue: 70, Saturation: 0.42, Kelvin: 3200},
			},
			Accents:     []Color{{Hue: 355, Saturation: 0.72, Kelvin: 2800}, {Hue: 95, Saturation: 0.55, Kelvin: 3400}},
			Backgrounds: []Color{{Hue: 36, Saturation: 0.22, Kelvin: 2200}, {Hue: 52, Saturation: 0.26, Kelvin: 3000}},
		},
		"neon": {
			Name: "neon",
			Base: []Color{
				{Hue: 145, Saturation: 1.0, Kelvin: 4000},
				{Hue: 300, Saturation: 1.0, Kelvin: 4000},
				{Hue: 55, Saturation: 1.0, Kelvin: 4000},
				{Hue: 190, Saturation: 1.0, Kelvin: 4200},
				{Hue: 20, Saturation: 0.95, Kelvin: 3300},
			},
			Accents:     []Color{{Hue: 0, Saturation: 1, Kelvin: 3200}, {Hue: 265, Saturation: 1, Kelvin: 4200}},
			Backgrounds: []Color{{Hue: 210, Saturation: 0.5, Kelvin: 4300}, {Hue: 300, Saturation: 0.42, Kelvin: 4000}},
		},
		"minimal": {
			Name: "minimal",
			Base: []Color{
				{Hue: 210, Saturation: 0.2, Kelvin: 4000},
				{Hue: 190, Saturation: 0.16, Kelvin: 4500},
				{Hue: 260, Saturation: 0.24, Kelvin: 3800},
				{Hue: 42, Saturation: 0.18, Kelvin: 3000},
			},
			Accents:     []Color{{Hue: 260, Saturation: 0.32, Kelvin: 3800}},
			Backgrounds: []Color{{Hue: 210, Saturation: 0.08, Kelvin: 4200}, {Hue: 45, Saturation: 0.08, Kelvin: 3200}},
		},
		"cyberpunk": {
			Name: "cyberpunk",
			Base: []Color{
				{Hue: 310, Saturation: 1.0, Kelvin: 3600},
				{Hue: 95, Saturation: 0.95, Kelvin: 4300},
				{Hue: 180, Saturation: 1.0, Kelvin: 4200},
				{Hue: 48, Saturation: 1.0, Kelvin: 3300},
				{Hue: 265, Saturation: 1.0, Kelvin: 4200},
			},
			Accents:     []Color{{Hue: 350, Saturation: 1, Kelvin: 3200}, {Hue: 150, Saturation: 1, Kelvin: 4500}},
			Backgrounds: []Color{{Hue: 275, Saturation: 0.45, Kelvin: 3600}, {Hue: 190, Saturation: 0.52, Kelvin: 4300}},
		},
	}
}

func firstOr(colors []Color, fallback Color) Color {
	if len(colors) == 0 {
		return fallback
	}
	return colors[0]
}
