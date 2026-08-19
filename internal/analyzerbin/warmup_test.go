package analyzerbin

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteWarmupAudioProducesAPlayableWAV(t *testing.T) {
	path, err := writeWarmupAudio()
	if err != nil {
		t.Fatalf("writeWarmupAudio: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read warmup audio: %v", err)
	}

	expectedData := warmupSampleRate * warmupSeconds * 2
	if len(data) != 44+expectedData {
		t.Fatalf("expected %d bytes, got %d", 44+expectedData, len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file: %q", data[0:12])
	}

	// The declared sizes must agree with the payload, or decoders reject the file.
	if got := binary.LittleEndian.Uint32(data[4:8]); got != uint32(36+expectedData) {
		t.Fatalf("riff size %d does not match payload", got)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != uint32(expectedData) {
		t.Fatalf("data size %d does not match payload", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != warmupSampleRate {
		t.Fatalf("sample rate %d", got)
	}
	if got := binary.LittleEndian.Uint16(data[34:36]); got != 16 {
		t.Fatalf("bits per sample %d", got)
	}

	// A tone, not silence: silence lets the analyzer skip the paths we warm.
	var loudest int16
	for i := 44; i+1 < len(data); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(data[i:]))
		if sample > loudest {
			loudest = sample
		}
	}
	if loudest < 1000 {
		t.Fatalf("warmup audio is effectively silent, peak %d", loudest)
	}
}

// The warm marker gates both the warmup and, later, the first-run intro, so its
// lifecycle has to be exact.
func TestWarmMarkerLifecycle(t *testing.T) {
	if !Bundled() {
		if NeedsWarmup() {
			t.Fatal("development build reported a pending warmup")
		}
		if err := MarkWarm(); err != nil {
			t.Fatalf("MarkWarm should be a no-op without a bundle: %v", err)
		}
		t.Skip("no bundled analyzer to warm")
	}

	t.Setenv("HOME", t.TempDir())
	if !NeedsWarmup() {
		t.Fatal("expected a pending warmup in a fresh home directory")
	}

	baseDir, err := installDir()
	if err != nil {
		t.Fatalf("installDir: %v", err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("create base dir: %v", err)
	}
	if err := MarkWarm(); err != nil {
		t.Fatalf("MarkWarm: %v", err)
	}
	if NeedsWarmup() {
		t.Fatal("warmup recorded but NeedsWarmup still reports pending")
	}

	// A bundle bump pays the verification and JIT cost again.
	if err := os.WriteFile(warmFilePath(baseDir), []byte("v-old"), 0o644); err != nil {
		t.Fatalf("seed stale marker: %v", err)
	}
	if !NeedsWarmup() {
		t.Fatal("a marker from another version was accepted")
	}
}

// A warmup that never ran must stay pending, so the next launch retries it
// instead of leaving the cost to the user's first analysis.
func TestFailedWarmIsNotRecorded(t *testing.T) {
	if !Bundled() {
		t.Skip("the marker is only written for bundled builds")
	}
	t.Setenv("HOME", t.TempDir())

	if err := Warm(context.Background(), filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected a missing analyzer executable to fail")
	}
	if !NeedsWarmup() {
		t.Fatal("a failed warmup must not be recorded as complete")
	}
}
