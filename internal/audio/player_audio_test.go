package audio

import (
	"os"
	"testing"
	"time"
)

// Opt-in: this needs a real output device and makes ~2 seconds of sound, so it
// cannot run on CI. Set LIFX_MAESTRO_AUDIO_TEST=1 to run it.
//
// The speaker opens once per process at the first file's rate, so a file encoded
// at a different rate must be resampled or it plays at the wrong speed and pitch.
// Opening a 44.1kHz file first and then playing a 48kHz one reproduces that: the
// media position should still advance in real time, where an unresampled stream
// advances at 44100/48000 of it, about 0.92.
func TestResampledPlaybackKeepsRealTime(t *testing.T) {
	if os.Getenv("LIFX_MAESTRO_AUDIO_TEST") == "" {
		t.Skip("set LIFX_MAESTRO_AUDIO_TEST=1 to run playback tests")
	}

	first, err := NewBeepPlayer("../../samples/lofi.mp3")
	if err != nil {
		t.Fatalf("open 44.1kHz file: %v", err)
	}
	defer first.Stop()

	second, err := NewBeepPlayer("../../samples/metal1.mp3")
	if err != nil {
		t.Fatalf("open 48kHz file: %v", err)
	}
	defer second.Stop()

	if err := second.Play(); err != nil {
		t.Fatalf("play: %v", err)
	}
	start := time.Now()
	time.Sleep(2 * time.Second)

	elapsed := time.Since(start)
	ratio := second.Position().Seconds() / elapsed.Seconds()
	t.Logf("elapsed=%.2fs position=%.2fs ratio=%.3f", elapsed.Seconds(), second.Position().Seconds(), ratio)
	if ratio < 0.97 || ratio > 1.03 {
		t.Fatalf("playback ran at %.1f%% of real time; is the stream being resampled?", ratio*100)
	}
}
