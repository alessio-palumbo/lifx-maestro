package palette

type Color struct {
	Hue        float64
	Saturation float64
	Kelvin     int
}

type Palette interface {
	Primary() Color
	Secondary() Color
	Accent() Color
}

type Fixed struct {
	NameValue      string
	PrimaryColor   Color
	SecondaryColor Color
	AccentColor    Color
}

func (p Fixed) Primary() Color {
	return p.PrimaryColor
}

func (p Fixed) Secondary() Color {
	return p.SecondaryColor
}

func (p Fixed) Accent() Color {
	return p.AccentColor
}

func (p Fixed) Name() string {
	return p.NameValue
}

func All() map[string]Fixed {
	return map[string]Fixed{
		"synthwave": {
			NameValue:      "synthwave",
			PrimaryColor:   Color{Hue: 285, Saturation: 1.0, Kelvin: 3500},
			SecondaryColor: Color{Hue: 200, Saturation: 1.0, Kelvin: 3500},
			AccentColor:    Color{Hue: 330, Saturation: 1.0, Kelvin: 3500},
		},
		"cinematic": {
			NameValue:      "cinematic",
			PrimaryColor:   Color{Hue: 220, Saturation: 0.72, Kelvin: 3200},
			SecondaryColor: Color{Hue: 38, Saturation: 0.65, Kelvin: 2700},
			AccentColor:    Color{Hue: 12, Saturation: 0.82, Kelvin: 3000},
		},
		"warm": {
			NameValue:      "warm",
			PrimaryColor:   Color{Hue: 28, Saturation: 0.55, Kelvin: 2600},
			SecondaryColor: Color{Hue: 48, Saturation: 0.48, Kelvin: 3000},
			AccentColor:    Color{Hue: 355, Saturation: 0.72, Kelvin: 2800},
		},
		"neon": {
			NameValue:      "neon",
			PrimaryColor:   Color{Hue: 145, Saturation: 1.0, Kelvin: 4000},
			SecondaryColor: Color{Hue: 300, Saturation: 1.0, Kelvin: 4000},
			AccentColor:    Color{Hue: 55, Saturation: 1.0, Kelvin: 4000},
		},
		"minimal": {
			NameValue:      "minimal",
			PrimaryColor:   Color{Hue: 210, Saturation: 0.2, Kelvin: 4000},
			SecondaryColor: Color{Hue: 190, Saturation: 0.16, Kelvin: 4500},
			AccentColor:    Color{Hue: 260, Saturation: 0.32, Kelvin: 3800},
		},
		"cyberpunk": {
			NameValue:      "cyberpunk",
			PrimaryColor:   Color{Hue: 310, Saturation: 1.0, Kelvin: 3600},
			SecondaryColor: Color{Hue: 95, Saturation: 0.95, Kelvin: 4300},
			AccentColor:    Color{Hue: 180, Saturation: 1.0, Kelvin: 4200},
		},
	}
}
