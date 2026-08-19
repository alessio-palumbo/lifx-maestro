package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	// BinaryPath runs a self-contained analyzer executable. When set it takes
	// precedence over PythonPath/ScriptPath, which are the development fallback.
	BinaryPath string
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
	if a.BinaryPath == "" {
		if a.PythonPath == "" {
			a.PythonPath = "python3"
		}
		if a.ScriptPath == "" {
			a.ScriptPath = defaultScriptPath()
		}
	}
	if a.Timeout <= 0 {
		a.Timeout = 2 * time.Minute
	}

	// Resolve the path before choosing the child's directory below, or a relative
	// path from the command line would stop resolving.
	audioPath, err := filepath.Abs(audioPath)
	if err != nil {
		return nil, fmt.Errorf("resolve audio path: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()

	cmd := a.command(ctx, audioPath)
	// Run from a directory of our choosing rather than inheriting the launcher's.
	// An app started from Finder inherits "/", and the frozen analyzer has been
	// seen to die there with a segmentation fault inside numpy — a null function
	// pointer in its ufunc machinery. The CLI never hit it because a shell always
	// supplies a normal directory. Nothing about the analyzer wants the caller's
	// directory: it finds its own libraries from the executable path, and the audio
	// path above is absolute.
	cmd.Dir = os.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("analyze audio timed out: %w", ctx.Err())
		}
		return nil, a.runError(err, stderr.String())
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

func (a Analyzer) command(ctx context.Context, audioPath string) *exec.Cmd {
	if a.BinaryPath != "" {
		return exec.CommandContext(ctx, a.BinaryPath, audioPath)
	}
	return exec.CommandContext(ctx, a.PythonPath, a.ScriptPath, audioPath)
}

// runError explains the two failures users actually hit: no interpreter on the
// machine, and an interpreter without the analyzer's dependencies.
func (a Analyzer) runError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)

	if a.BinaryPath != "" {
		if stderr == "" {
			return fmt.Errorf("run bundled analyzer %s: %w", a.BinaryPath, err)
		}
		return fmt.Errorf("run bundled analyzer %s: %w: %s", a.BinaryPath, err, stderr)
	}

	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("python interpreter %q not found: install Python 3 and the analyzer dependencies (pip install -r analyzer/requirements.txt)", a.PythonPath)
	}
	if strings.Contains(stderr, "ModuleNotFoundError") || strings.Contains(stderr, "No module named") {
		return fmt.Errorf("python interpreter %s is missing analyzer dependencies: run %s -m pip install -r analyzer/requirements.txt (details: %s)", a.PythonPath, a.PythonPath, lastLine(stderr))
	}
	if stderr == "" {
		return fmt.Errorf("run analyzer %s %s: %w", a.PythonPath, a.ScriptPath, err)
	}
	return fmt.Errorf("run analyzer: %w: %s", err, stderr)
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
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
		return filepath.Join("analyzer", "analyze.py")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "analyzer", "analyze.py")
}
