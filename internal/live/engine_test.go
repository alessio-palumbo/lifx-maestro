package live

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/timeline"
)

type chunkSource struct {
	chunks []PCMChunk
	err    error
}

func (s chunkSource) Name() string { return "test PCM" }

func (s chunkSource) Run(ctx context.Context, output chan<- PCMChunk) error {
	for _, chunk := range s.chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case output <- chunk:
		}
	}
	return s.err
}

type waitingSource struct{}

func (waitingSource) Name() string { return "waiting" }

func (waitingSource) Run(ctx context.Context, _ chan<- PCMChunk) error {
	<-ctx.Done()
	return ctx.Err()
}

type windowAnalyzer struct {
	mu      sync.Mutex
	windows []PCMWindow
	closed  bool
}

func (a *windowAnalyzer) Analyze(_ context.Context, window PCMWindow) (Features, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.windows = append(a.windows, window)
	return Features{
		At:            window.End,
		RMSDB:         -12,
		LowDB:         -14,
		MidDB:         -20,
		HighDB:        -24,
		OnsetStrength: 0.8,
	}, nil
}

func (a *windowAnalyzer) Close() error {
	a.closed = true
	return nil
}

type collectingSink struct {
	mu     sync.Mutex
	events []timeline.Event
}

func (s *collectingSink) Run(ctx context.Context, input <-chan []timeline.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case events, ok := <-input:
			if !ok {
				return nil
			}
			s.mu.Lock()
			s.events = append(s.events, events...)
			s.mu.Unlock()
		}
	}
}

func TestEngineRunsRollingAnalysisAndGeneration(t *testing.T) {
	analyzer := &windowAnalyzer{}
	sink := &collectingSink{}
	generator, err := NewGenerator(GeneratorConfig{
		Devices: []devices.DeviceInfo{{ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{
		Source: chunkSource{chunks: []PCMChunk{
			{Samples: []float32{1, 2}, SampleRate: 10, CapturedAt: 200 * time.Millisecond},
			{Samples: []float32{3, 4}, SampleRate: 10, CapturedAt: 400 * time.Millisecond},
			{Samples: []float32{5, 6}, SampleRate: 10, CapturedAt: 600 * time.Millisecond},
		}},
		Analyzer:       analyzer,
		Generator:      generator,
		Sink:           sink,
		WindowDuration: 400 * time.Millisecond,
		HopDuration:    200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(analyzer.windows) != 2 {
		t.Fatalf("analyzed windows = %d, want 2", len(analyzer.windows))
	}
	if !analyzer.closed {
		t.Fatal("analyzer was not closed")
	}
	if len(sink.events) == 0 {
		t.Fatal("live generator produced no device events")
	}
}

func TestEngineRejectsHopLongerThanWindow(t *testing.T) {
	_, err := NewEngine(EngineConfig{
		Source:         chunkSource{},
		Analyzer:       &windowAnalyzer{},
		Generator:      &Generator{},
		Sink:           &collectingSink{},
		WindowDuration: 100 * time.Millisecond,
		HopDuration:    200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected invalid window configuration error")
	}
}

func TestEngineReturnsSourceErrorAfterBufferedAudioDrains(t *testing.T) {
	wantErr := errors.New("capture failed")
	generator, err := NewGenerator(GeneratorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{
		Source:    chunkSource{err: wantErr},
		Analyzer:  &windowAnalyzer{},
		Generator: generator,
		Sink:      &collectingSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
}

func TestEngineCancellationStopsAllComponents(t *testing.T) {
	analyzer := &windowAnalyzer{}
	generator, err := NewGenerator(GeneratorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{
		Source:    waitingSource{},
		Analyzer:  analyzer,
		Generator: generator,
		Sink:      &collectingSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("engine did not stop after cancellation")
	}
	if !analyzer.closed {
		t.Fatal("analyzer was not closed after cancellation")
	}
}
