package timeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Timeline struct {
	Name       string  `json:"name"`
	DurationMS int64   `json:"duration_ms"`
	Events     []Event `json:"events"`
}

type Event struct {
	TimeMS int64                  `json:"time_ms"`
	Target string                 `json:"target"`
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params,omitempty"`
}

func Load(path string) (*Timeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read timeline: %w", err)
	}

	var tl Timeline
	if err := json.Unmarshal(data, &tl); err != nil {
		return nil, fmt.Errorf("parse timeline JSON: %w", err)
	}

	if err := tl.Validate(); err != nil {
		return nil, err
	}

	tl.SortEvents()
	return &tl, nil
}

func Save(path string, tl *Timeline) error {
	if tl == nil {
		return fmt.Errorf("timeline is required")
	}
	if err := tl.Validate(); err != nil {
		return err
	}
	tl.SortEvents()

	data, err := json.MarshalIndent(tl, "", "  ")
	if err != nil {
		return fmt.Errorf("encode timeline JSON: %w", err)
	}
	data = append(data, '\n')

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write timeline: %w", err)
	}
	return nil
}

func (t *Timeline) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("timeline name is required")
	}
	if t.DurationMS < 0 {
		return fmt.Errorf("duration_ms must be non-negative")
	}

	for i, event := range t.Events {
		if event.TimeMS < 0 {
			return fmt.Errorf("event %d: time_ms must be non-negative", i)
		}
		if t.DurationMS > 0 && event.TimeMS > t.DurationMS {
			return fmt.Errorf("event %d: time_ms exceeds duration_ms", i)
		}
		if event.Target == "" {
			return fmt.Errorf("event %d: target is required", i)
		}
		if event.Action == "" {
			return fmt.Errorf("event %d: action is required", i)
		}
	}

	return nil
}

func (t *Timeline) SortEvents() {
	sort.SliceStable(t.Events, func(i, j int) bool {
		return t.Events[i].TimeMS < t.Events[j].TimeMS
	})
}
