package sections

import "lifx-maestro/internal/analysis"

type Type string

const (
	TypeIntro     Type = "intro"
	TypeBuild     Type = "build"
	TypeDrop      Type = "drop"
	TypeBreakdown Type = "breakdown"
	TypeOutro     Type = "outro"
)

type Section struct {
	StartMS int64
	EndMS   int64
	Type    Type
	Energy  float64
}

func FromAnalysis(song analysis.SongAnalysis) []Section {
	if len(song.Sections) > 0 {
		sections := make([]Section, 0, len(song.Sections))
		for _, section := range song.Sections {
			sections = append(sections, Section{
				StartMS: section.StartMS,
				EndMS:   section.EndMS,
				Type:    Type(section.Type),
				Energy:  clamp(section.Energy, 0, 1),
			})
		}
		return sections
	}

	return fallback(song)
}

func fallback(song analysis.SongAnalysis) []Section {
	bounds := []struct {
		start float64
		end   float64
		typ   Type
	}{
		{0.00, 0.12, TypeIntro},
		{0.12, 0.18, TypeBuild},
		{0.18, 0.50, TypeDrop},
		{0.50, 0.62, TypeBreakdown},
		{0.62, 0.72, TypeBuild},
		{0.72, 0.88, TypeDrop},
		{0.88, 1.00, TypeOutro},
	}

	sections := make([]Section, 0, len(bounds))
	for _, bound := range bounds {
		start := int64(float64(song.DurationMS) * bound.start)
		end := int64(float64(song.DurationMS) * bound.end)
		sections = append(sections, Section{
			StartMS: start,
			EndMS:   end,
			Type:    bound.typ,
			Energy:  meanEnergy(song.Energy, start, end),
		})
	}
	return sections
}

func meanEnergy(points []analysis.EnergyPoint, startMS, endMS int64) float64 {
	var total float64
	var count int
	for _, point := range points {
		if point.TimeMS >= startMS && point.TimeMS < endMS {
			total += point.Value
			count++
		}
	}
	if count == 0 {
		return 0.5
	}
	return clamp(total/float64(count), 0, 1)
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
