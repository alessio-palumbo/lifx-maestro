package main

import "testing"

func TestCommandTreeIncludesExpectedCommands(t *testing.T) {
	cmd := newCommand()
	got := make(map[string]bool)
	for _, subcommand := range cmd.Commands {
		got[subcommand.Name] = true
	}

	for _, name := range []string{"analyze", "generate", "perform", "play"} {
		if !got[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}
