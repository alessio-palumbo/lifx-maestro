package main

import (
	"reflect"
	"testing"
)

func TestInterspersedFlagsMovesFlagsBeforePositionals(t *testing.T) {
	got := interspersedFlags(
		[]string{"song.mp3", "--output", "projects/song.json", "--mode=ambient"},
		map[string]bool{"output": true, "mode": true},
	)
	want := []string{"--output", "projects/song.json", "--mode=ambient", "song.mp3"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
