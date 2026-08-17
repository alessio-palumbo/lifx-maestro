package generation

import (
	"encoding/json"

	"lifx-maestro/internal/timeline"
)

const minTransitionDurationMS int64 = 20

func normalizeTimelineEvents(events []timeline.Event) []timeline.Event {
	if len(events) == 0 {
		return events
	}

	grouped := make(map[string][]int)
	for index, event := range events {
		key, ok := transitionGroupKey(event)
		if !ok {
			continue
		}
		grouped[key] = append(grouped[key], index)
	}

	keep := make([]bool, len(events))
	for i := range keep {
		keep[i] = true
	}

	for _, indexes := range grouped {
		for i := 0; i < len(indexes)-1; i++ {
			currentIndex := indexes[i]
			next := events[indexes[i+1]]
			current := events[currentIndex]
			duration, ok := eventDurationMS(current)
			if !ok || duration <= 0 {
				continue
			}
			available := next.TimeMS - current.TimeMS
			if available >= duration {
				continue
			}
			if available < minTransitionDurationMS {
				keep[currentIndex] = false
				continue
			}
			events[currentIndex] = withDurationMS(current, available)
		}
	}

	normalized := make([]timeline.Event, 0, len(events))
	for index, event := range events {
		if keep[index] {
			normalized = append(normalized, event)
		}
	}
	return normalized
}

func transitionGroupKey(event timeline.Event) (string, bool) {
	switch event.Action {
	case "set_color", "set_zone_colors", "set_matrix_colors":
		return event.Target + "\x00" + event.Action, true
	default:
		return "", false
	}
}

func eventDurationMS(event timeline.Event) (int64, bool) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(event.Params, &params); err != nil {
		return 0, false
	}
	raw, ok := params["duration_ms"]
	if !ok {
		return 0, false
	}
	var duration int64
	if err := json.Unmarshal(raw, &duration); err != nil {
		return 0, false
	}
	return duration, true
}

func withDurationMS(event timeline.Event, durationMS int64) timeline.Event {
	var params map[string]any
	if err := json.Unmarshal(event.Params, &params); err != nil {
		return event
	}
	params["duration_ms"] = durationMS
	data, err := json.Marshal(params)
	if err != nil {
		return event
	}
	event.Params = data
	return event
}

func hasTransitionOverlap(events []timeline.Event) bool {
	lastEnd := make(map[string]int64)
	for _, event := range events {
		key, ok := transitionGroupKey(event)
		if !ok {
			continue
		}
		if event.TimeMS < lastEnd[key] {
			return true
		}
		if duration, ok := eventDurationMS(event); ok {
			lastEnd[key] = event.TimeMS + duration
			continue
		}
		lastEnd[key] = event.TimeMS
	}
	return false
}
