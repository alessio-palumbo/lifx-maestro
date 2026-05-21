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

func TestDefaultPythonPathPrefersProjectVenv(t *testing.T) {
	if got := defaultPythonPath(); got == "" {
		t.Fatal("default python path is empty")
	}
}
