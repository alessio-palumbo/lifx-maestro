package audiodsp

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func tone(rate, count int, hz, amplitude float64) []float32 {
	pcm := make([]float32, count)
	for i := range pcm {
		pcm[i] = float32(amplitude * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
	}
	return pcm
}

func TestToneBandAndRMSScaling(t *testing.T) {
	for _, hz := range []float64{100, 1000, 6000} {
		t.Run(fmtHz(hz), func(t *testing.T) {
			pcm := tone(16000, 8000, hz, 0.1)
			f, err := New().Analyze(context.Background(), Window{Samples: pcm, SampleRate: 16000, End: 500 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			want := 20 * math.Log10(0.1/math.Sqrt2)
			if math.Abs(f.RMSDB-want) > 0.001 {
				t.Fatalf("RMS = %.4f, want %.4f", f.RMSDB, want)
			}
			values := []float64{f.LowDB, f.MidDB, f.HighDB}
			band := 0
			if hz >= 250 {
				band = 1
			}
			if hz >= 4000 {
				band = 2
			}
			if math.Abs(values[band]-want) > 0.01 {
				t.Fatalf("band = %.4f, want %.4f", values[band], want)
			}
			for i, value := range values {
				if i != band && value > want-50 {
					t.Fatalf("tone leaked into band %d: %.4f", i, value)
				}
			}
		})
	}
}
func fmtHz(hz float64) string {
	if hz == 100 {
		return "low"
	}
	if hz == 1000 {
		return "mid"
	}
	return "high"
}

func TestRecentWindowDetectsSilenceBeforeWholeWindow(t *testing.T) {
	pcm := tone(16000, 8000, 1000, 0.2)
	clear(pcm[6400:])
	f, err := New().Analyze(context.Background(), Window{Samples: pcm, SampleRate: 16000, End: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if f.RMSDB < -30 || f.RecentRMSDB != -120 || f.RecentMidDB != -120 {
		t.Fatalf("recent silence did not separate from retained music: %+v", f)
	}
}

func TestSilenceNeverProducesOnsetOrTempo(t *testing.T) {
	a := New()
	for i := 0; i < 200; i++ {
		f, err := a.Analyze(context.Background(), Window{Samples: make([]float32, 8000), SampleRate: 16000, End: time.Duration(i+5) * 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if f.Onset || f.Beat || f.TempoBPM != 0 || f.RMSDB != -120 {
			t.Fatalf("silence produced musical features: %+v", f)
		}
	}
	if len(a.flux) != 320 || len(a.times) != 320 {
		t.Fatalf("unbounded history: %d/%d", len(a.flux), len(a.times))
	}
}

func TestRhythmicBurstsHaveTempoSupport(t *testing.T) {
	a := New()
	var f Features
	for step := 5; step <= 140; step++ {
		pcm := make([]float32, 8000)
		for i := range pcm {
			seconds := float64(step)/10 - 0.5 + float64(i)/16000
			phase := math.Mod(seconds, 0.5)
			if phase < 0.08 {
				pcm[i] = float32(0.25 * math.Exp(-phase*30) * math.Sin(2*math.Pi*110*seconds))
			}
		}
		var err error
		f, err = a.Analyze(context.Background(), Window{Samples: pcm, SampleRate: 16000, End: time.Duration(step) * 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
	}
	found := false
	for _, c := range f.TempoCandidates {
		if math.Abs(c.BPM-120) < 3 && c.Confidence > 0.5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("120 BPM bursts not represented: %+v", f)
	}
}

func TestRejectsInvalidInputAndHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Analyze(ctx, Window{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	for _, w := range []Window{{}, {Samples: []float32{1}, SampleRate: 0}, {Samples: []float32{float32(math.NaN())}, SampleRate: 16000}, {Samples: []float32{float32(math.Inf(1))}, SampleRate: 16000}} {
		if _, err := New().Analyze(context.Background(), w); err == nil {
			t.Fatalf("accepted invalid window: %+v", w)
		}
	}
	a := New()
	w := Window{Samples: []float32{0}, SampleRate: 16000, End: time.Second}
	if _, err := a.Analyze(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Analyze(context.Background(), w); err == nil {
		t.Fatal("duplicate timestamp accepted")
	}
	w.End += time.Second
	w.SampleRate = 44100
	if _, err := a.Analyze(context.Background(), w); err == nil {
		t.Fatal("sample-rate change accepted")
	}
}

func BenchmarkAnalyze16000(b *testing.B) {
	a := New()
	pcm := tone(16000, 8000, 110, 0.1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := a.Analyze(context.Background(), Window{Samples: pcm, SampleRate: 16000, End: time.Duration(i+5) * 100 * time.Millisecond}); err != nil {
			b.Fatal(err)
		}
	}
}
