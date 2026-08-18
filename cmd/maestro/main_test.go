package main

import "testing"

func TestCommandTreeIncludesExpectedCommands(t *testing.T) {
	cmd := newCommand()
	got := make(map[string]bool)
	for _, subcommand := range cmd.Commands {
		got[subcommand.Name] = true
	}

	for _, name := range []string{"analyze", "devices", "generate", "perform", "play", "styles"} {
		if !got[name] {
			t.Fatalf("missing command %q", name)
		}
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
	if flags["devices"] {
		t.Fatal("devices flag should not be exposed")
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
