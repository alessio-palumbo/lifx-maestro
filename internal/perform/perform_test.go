package perform

import (
	"os"
	"path/filepath"
	"testing"

	"lifx-maestro/internal/analysis"
)

// An unset Options.Analyzer must resolve the analyzer this build ships with,
// never a script path that is only valid on the machine that compiled it.
func TestResolveAnalyzerWithoutCallerAnalyzer(t *testing.T) {
	analyzer, err := resolveAnalyzer(Options{})
	if err != nil {
		t.Fatalf("resolveAnalyzer: %v", err)
	}

	if analyzer.BinaryPath != "" {
		if _, err := os.Stat(analyzer.BinaryPath); err != nil {
			t.Fatalf("bundled analyzer not installed: %v", err)
		}
		return
	}

	if analyzer.PythonPath == "" {
		t.Fatal("expected a python interpreter for the development fallback")
	}
	if !filepath.IsAbs(analyzer.ScriptPath) {
		t.Fatalf("script path must be absolute, got %q", analyzer.ScriptPath)
	}
	if _, err := os.Stat(analyzer.ScriptPath); err != nil {
		t.Fatalf("analyzer script not found: %v", err)
	}
}

func TestResolveAnalyzerKeepsCallerAnalyzer(t *testing.T) {
	supplied := analysis.Analyzer{PythonPath: "/usr/bin/python3", ScriptPath: "/tmp/analyze.py"}

	analyzer, err := resolveAnalyzer(Options{Analyzer: supplied})
	if err != nil {
		t.Fatalf("resolveAnalyzer: %v", err)
	}
	if analyzer != supplied {
		t.Fatalf("caller analyzer was replaced: got %+v", analyzer)
	}
}

func TestResolveAnalyzerKeepsCallerBundledBinary(t *testing.T) {
	supplied := analysis.Analyzer{BinaryPath: "/opt/analyze"}

	analyzer, err := resolveAnalyzer(Options{Analyzer: supplied})
	if err != nil {
		t.Fatalf("resolveAnalyzer: %v", err)
	}
	if analyzer.BinaryPath != supplied.BinaryPath {
		t.Fatalf("caller binary path was replaced: got %q", analyzer.BinaryPath)
	}
}
