package recording

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/live"
)

type sourceFunc func(context.Context, chan<- live.PCMChunk) error

func (sourceFunc) Name() string                                              { return "fake" }
func (f sourceFunc) Run(ctx context.Context, out chan<- live.PCMChunk) error { return f(ctx, out) }

func TestCaptureLimitsSamplesAndJoinsSource(t *testing.T) {
	exited := make(chan struct{})
	source := sourceFunc(func(ctx context.Context, out chan<- live.PCMChunk) error {
		defer close(exited)
		select {
		case out <- live.PCMChunk{SampleRate: 10, Samples: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}}:
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return ctx.Err()
	})
	pcm, err := Capture(context.Background(), source, 10, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm.Samples) != 5 || pcm.Samples[4] != 0.5 {
		t.Fatalf("recording = %+v", pcm)
	}
	select {
	case <-exited:
	default:
		t.Fatal("capture returned before source cleanup")
	}
}

func TestCancellationReturnsPartialRecording(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := sourceFunc(func(ctx context.Context, out chan<- live.PCMChunk) error {
		out <- live.PCMChunk{SampleRate: 10, Samples: []float32{0.25}}
		// A second send on the small queue does not prove the first was consumed.
		// Wait for the first receive before requesting the simulated Ctrl-C.
		for len(out) > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				time.Sleep(time.Millisecond)
			}
		}
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	pcm, err := Capture(ctx, source, 10, time.Second)
	if !errors.Is(err, context.Canceled) || len(pcm.Samples) != 1 {
		t.Fatalf("partial capture = %+v, %v", pcm, err)
	}
}

func TestCaptureSurfacesSourceFailures(t *testing.T) {
	want := errors.New("microphone unavailable")
	_, err := Capture(context.Background(), sourceFunc(func(context.Context, chan<- live.PCMChunk) error { return want }), 16000, time.Second)
	if !errors.Is(err, want) {
		t.Fatalf("capture failure = %v", err)
	}
}
