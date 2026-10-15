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
	want := []float64{0.5, -0.1, 0.1, -0.4}
	for i, sample := range pcm.Samples {
		if math.Abs(float64(sample)-want[i]) > 0.001 {
			t.Fatalf("sample %d = %.3f, want %.3f", i, sample, want[i])
		}
	}
}

func TestRecordedWAVRoundTripPreservesLevelAndFinalPartialBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.wav")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]float32, 1027)
	levels := []float32{0.25, -0.125, 0.001, -0.0001, 0}
	for i := range samples {
		samples[i] = levels[i%len(levels)]
	}
	if err := WritePCM(file, &PCMData{Samples: samples, SampleRate: 16000}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePCM(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SampleRate != 16000 || len(decoded.Samples) != len(samples) {
		t.Fatalf("recording shape = %d/%d", decoded.SampleRate, len(decoded.Samples))
	}
	for i, want := range samples {
		if math.Abs(float64(decoded.Samples[i]-want)) > 3e-7 {
			t.Fatalf("sample %d: %.8f != %.8f", i, decoded.Samples[i], want)
		}
	}
}
