package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	livemode "lifx-maestro/internal/live"
)

func TestCommandTreeIncludesExpectedCommands(t *testing.T) {
	cmd := newCommand()
	got := make(map[string]bool)
	for _, subcommand := range cmd.Commands {
		got[subcommand.Name] = true
	}

	for _, name := range []string{"analyze", "devices", "evaluate-live", "generate", "live", "perform", "play", "styles"} {
		if !got[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}

func TestEvaluateLiveUsesSyntheticDeviceKinds(t *testing.T) {
	for _, kind := range []string{"single_zone", "multizone", "matrix", "all"} {
		infos, err := evaluationDevices(kind)
		if err != nil {
			t.Fatalf("evaluationDevices(%q): %v", kind, err)
		}
		if len(infos) == 0 {
			t.Fatalf("evaluationDevices(%q) returned no devices", kind)
		}
	}
	if _, err := evaluationDevices("beam"); err == nil {
		t.Fatal("unsupported synthetic device kind was accepted")
	}
}

func TestPerformUsesTargetFlag(t *testing.T) {
	cmd := performCommand()
	flags := make(map[string]bool)
	for _, flag := range cmd.Flags {
		for _, name := range flag.Names() {
			flags[name] = true
		}
	}

	if !flags["target"] {
		t.Fatal("missing target flag")
	}
	if !flags["generation"] {
		t.Fatal("missing generation flag")
	}
	if !flags["intensity"] {
		t.Fatal("missing intensity flag")
	}
	if flags["devices"] {
		t.Fatal("devices flag should not be exposed")
	}
}

func TestLiveExposesSensitivityAndIntensityFlags(t *testing.T) {
	flags := make(map[string]bool)
	for _, flag := range liveCommand().Flags {
		for _, name := range flag.Names() {
			flags[name] = true
		}
	}
	for _, name := range []string{"target", "input", "style", "intensity", "sensitivity"} {
		if !flags[name] {
			t.Fatalf("missing live flag %q", name)
		}
	}
}

func TestNewAnalyzerResolvesARunnableAnalyzer(t *testing.T) {
	analyzer, err := newAnalyzer(analyzeCommand())
	if err != nil {
		t.Fatalf("newAnalyzer: %v", err)
	}
	if analyzer.BinaryPath == "" && analyzer.PythonPath == "" {
		t.Fatal("analyzer has neither a bundled binary nor a python interpreter")
	}
}

func TestNewAnalyzerHonoursPythonOverride(t *testing.T) {
	cmd := analyzeCommand()
	if err := cmd.Set("python", "/usr/bin/python3"); err != nil {
		t.Fatalf("set python flag: %v", err)
	}

	analyzer, err := newAnalyzer(cmd)
	if err != nil {
		t.Fatalf("newAnalyzer: %v", err)
	}
	if analyzer.PythonPath != "/usr/bin/python3" {
		t.Fatalf("python override ignored: got %q", analyzer.PythonPath)
	}
	if analyzer.BinaryPath != "" {
		t.Fatal("python override should bypass the bundled analyzer")
	}
	if analyzer.ScriptPath == "" {
		t.Fatal("python override needs an analyzer script path")
	}
}

func TestNewAnalyzerResolvesRelativePythonOverride(t *testing.T) {
	cmd := analyzeCommand()
	if err := cmd.Set("python", "analyzer/.venv/bin/python"); err != nil {
		t.Fatal(err)
	}
	analyzer, err := newAnalyzer(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(analyzer.PythonPath) {
		t.Fatalf("Python path = %q, want absolute", analyzer.PythonPath)
	}
}

func TestLiveDiagnosticsExposeDecisionInputs(t *testing.T) {
	var output bytes.Buffer
	diagnostics := &liveDiagnostics{source: livemode.NewMicrophoneSource(livemode.MicrophoneConfig{}), target: "tv", out: &output}
	diagnostics.ObserveOutput(livemode.OutputActivity{At: 500 * time.Millisecond, GeneratedEvents: 3, DroppedEvents: 1})
	diagnostics.Observe(livemode.State{
		At:              500 * time.Millisecond,
		InputDB:         -34.2,
		NoiseFloorDB:    -58.6,
		MarginDB:        24.4,
		Energy:          0.62,
		Presence:        0.73,
		Low:             0.22,
		Mid:             0.62,
		High:            0.92,
		TempoBPM:        85.7,
		TempoConfidence: 0.71,
		Active:          true,
		Sustained:       true,
		Activity:        0.52,
		Intensity:       0.67,
		Dynamics:        livemode.DynamicsEnergetic,
		Novelty:         0.34,
		SectionChange:   true,
		Onset:           true,
	})
	line := output.String()
	for _, expected := range []string{
		"level=-34.2dB", "floor=-58.6dB", "margin=24.4dB", "energy=0.62", "presence=0.73", "tempo= 85.7", "confidence=0.71", "activity=0.52",
		"intensity=0.67", "dynamics=energetic", "novelty=0.34", "sections=1", "gate=open", "motion=ambient",
		"accents=1(onset=1 beat=0)", "generated=3", "sent=0", "replaced=1", "errors=0",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("diagnostics %q do not contain %q", line, expected)
		}
	}
}
