package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type SongAnalysis struct {
	DurationMS int64         `json:"duration_ms"`
	BPM        float64       `json:"bpm"`
	Beats      []int64       `json:"beats"`
	Energy     []EnergyPoint `json:"energy"`
	Sections   []Section     `json:"sections,omitempty"`
}

type EnergyPoint struct {
	TimeMS int64   `json:"time_ms"`
	Value  float64 `json:"value"`
}

type Section struct {
	StartMS int64   `json:"start_ms"`
	EndMS   int64   `json:"end_ms"`
	Type    string  `json:"type"`
	Energy  float64 `json:"energy"`
}

type Analyzer struct {
	PythonPath string
	ScriptPath string
	Timeout    time.Duration
}

func NewAnalyzer() Analyzer {
	return Analyzer{
		PythonPath: "python3",
		ScriptPath: defaultScriptPath(),
		Timeout:    2 * time.Minute,
	}
}

func (a Analyzer) Analyze(ctx context.Context, audioPath string) (*SongAnalysis, error) {
	if a.PythonPath == "" {
		a.PythonPath = "python3"
	}
	if a.ScriptPath == "" {
		a.ScriptPath = defaultScriptPath()
	}
	if a.Timeout <= 0 {
		a.Timeout = 2 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.PythonPath, a.ScriptPath, audioPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("analyze audio timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("run analyzer: %w: %s", err, stderr.String())
	}

	var result SongAnalysis
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parse analysis JSON: %w", err)
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s SongAnalysis) Validate() error {
	if s.DurationMS <= 0 {
		return fmt.Errorf("analysis duration_ms must be positive")
	}
	if s.BPM < 0 {
		return fmt.Errorf("analysis bpm must be non-negative")
	}
	for i, beat := range s.Beats {
		if beat < 0 {
			return fmt.Errorf("analysis beat %d must be non-negative", i)
		}
	}
	for i, point := range s.Energy {
		if point.TimeMS < 0 {
			return fmt.Errorf("analysis energy point %d time_ms must be non-negative", i)
		}
		if point.Value < 0 || point.Value > 1 {
			return fmt.Errorf("analysis energy point %d value must be between 0 and 1", i)
		}
	}
	for i, section := range s.Sections {
		if section.StartMS < 0 {
			return fmt.Errorf("analysis section %d start_ms must be non-negative", i)
		}
		if section.EndMS < section.StartMS {
			return fmt.Errorf("analysis section %d end_ms must be greater than or equal to start_ms", i)
		}
		if section.Type == "" {
			return fmt.Errorf("analysis section %d type is required", i)
		}
		if section.Energy < 0 || section.Energy > 1 {
			return fmt.Errorf("analysis section %d energy must be between 0 and 1", i)
		}
	}
	return nil
}

func defaultScriptPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("python", "analyze.py")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "python", "analyze.py")
}
