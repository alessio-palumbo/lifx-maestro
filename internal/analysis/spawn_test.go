package analysis

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeAnalyzer writes the working directory and audio argument it was given into
// reportDir, then prints a minimal valid analysis.
func fakeAnalyzer(t *testing.T, reportDir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is not portable to windows")
	}

	path := filepath.Join(t.TempDir(), "fake-analyzer")
	script := "#!/bin/sh\n" +
		"pwd > '" + reportDir + "/cwd'\n" +
		"printf '%s' \"$1\" > '" + reportDir + "/arg'\n" +
		`echo '{"duration_ms":1000,"bpm":120,"beats":[0],"energy":[{"time_ms":0,"value":0.5}]}'` + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// The analyzer must not inherit the caller's working directory. An app launched
// from Finder gets "/", where the frozen analyzer has crashed inside numpy.
func TestAnalyzeDoesNotInheritTheCallersDirectory(t *testing.T) {
	reports := t.TempDir()
	analyzer := Analyzer{BinaryPath: fakeAnalyzer(t, reports)}

	if _, err := analyzer.Analyze(context.Background(), "testdata/song.mp3"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	cwd, err := os.ReadFile(filepath.Join(reports, "cwd"))
	if err != nil {
		t.Fatalf("stub did not report its directory: %v", err)
	}
	if got := string(cwd); got == "/\n" {
		t.Fatal("analyzer ran from \"/\", the directory that crashes the bundled build")
	}
}

// Choosing the child's directory breaks relative paths unless they are resolved
// first, which is how the CLI is normally used.
func TestAnalyzeResolvesRelativeAudioPaths(t *testing.T) {
	reports := t.TempDir()
	analyzer := Analyzer{BinaryPath: fakeAnalyzer(t, reports)}

	if _, err := analyzer.Analyze(context.Background(), "testdata/song.mp3"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg, err := os.ReadFile(filepath.Join(reports, "arg"))
	if err != nil {
		t.Fatalf("stub did not report its argument: %v", err)
	}
	if !filepath.IsAbs(string(arg)) {
		t.Fatalf("analyzer received %q; a relative path cannot resolve from another directory", arg)
	}
}
