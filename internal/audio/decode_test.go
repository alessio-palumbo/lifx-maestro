package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/wav"
)

func TestDecodePCMConvertsStereoToMono(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.wav")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][2]float64{{0.8, 0.2}, {-0.4, 0.2}, {0.1, 0.1}, {-0.6, -0.2}}
	position := 0
	streamer := beep.StreamerFunc(func(output [][2]float64) (int, bool) {
		if position >= len(frames) {
			return 0, false
		}
		n := copy(output, frames[position:])
		position += n
		return n, true
	})
	format := beep.Format{SampleRate: 8000, NumChannels: 2, Precision: 2}
	if err := wav.Encode(file, streamer, format); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	pcm, err := DecodePCM(path)
	if err != nil {
		t.Fatal(err)
	}
	if pcm.SampleRate != 8000 || len(pcm.Samples) != len(frames) {
		t.Fatalf("decoded rate/length = %d/%d, want 8000/%d", pcm.SampleRate, len(pcm.Samples), len(frames))
	}
	// Beep's integer WAV encoder/decoder scales this fixture by one half. The
	// expected values still verify that DecodePCM averages both channels.
	want := []float64{0.25, -0.05, 0.05, -0.2}
	for i, sample := range pcm.Samples {
		if math.Abs(float64(sample)-want[i]) > 0.001 {
			t.Fatalf("sample %d = %.3f, want %.3f", i, sample, want[i])
		}
	}
}
