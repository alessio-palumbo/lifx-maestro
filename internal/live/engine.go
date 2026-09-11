package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"lifx-maestro/internal/timeline"
)

const (
	DefaultAnalysisWindow = 500 * time.Millisecond
	DefaultAnalysisHop    = 100 * time.Millisecond
)

type EngineConfig struct {
	Source    AudioSource
	Analyzer  Analyzer
	Tracker   *StateTracker
	Generator *Generator
	Sink      EventSink
	Observer  Observer

	WindowDuration time.Duration
	HopDuration    time.Duration
}

type Engine struct {
	config EngineConfig
}

func NewEngine(config EngineConfig) (*Engine, error) {
	if config.Source == nil {
		return nil, fmt.Errorf("live audio source is required")
	}
	if config.Analyzer == nil {
		return nil, fmt.Errorf("live analyzer is required")
	}
	if config.Generator == nil {
		return nil, fmt.Errorf("live generator is required")
	}
	if config.Sink == nil {
		return nil, fmt.Errorf("live event sink is required")
	}
	if config.Tracker == nil {
		config.Tracker = NewStateTracker(DefaultTrackerConfig())
	}
	if config.WindowDuration <= 0 {
		config.WindowDuration = DefaultAnalysisWindow
	}
	if config.HopDuration <= 0 {
		config.HopDuration = DefaultAnalysisHop
	}
	if config.HopDuration > config.WindowDuration {
		return nil, fmt.Errorf("live analysis hop must not exceed its window")
	}
	return &Engine{config: config}, nil
}

func (e *Engine) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer e.config.Analyzer.Close()

	chunks := make(chan PCMChunk, 8)
	eventBatches := make(chan []timeline.Event, 1)
	sourceDone := make(chan error, 1)
	sinkDone := make(chan error, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(chunks)
		err := e.config.Source.Run(ctx, chunks)
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		sourceDone <- err
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.config.Sink.Run(ctx, eventBatches)
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		sinkDone <- err
	}()

	var buffer *WindowBuffer
	var runErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-sinkDone:
			if err != nil {
				runErr = fmt.Errorf("dispatch live events: %w", err)
			}
			break loop
		case chunk, ok := <-chunks:
			if !ok {
				if err := <-sourceDone; err != nil {
					runErr = fmt.Errorf("capture audio: %w", err)
				}
				break loop
			}
			if buffer == nil {
				if chunk.SampleRate <= 0 {
					runErr = fmt.Errorf("live audio source returned an invalid sample rate")
					break loop
				}
				buffer = NewWindowBuffer(chunk.SampleRate, e.config.WindowDuration, e.config.HopDuration)
			} else if chunk.SampleRate != buffer.sampleRate {
				runErr = fmt.Errorf("live audio sample rate changed from %d to %d", buffer.sampleRate, chunk.SampleRate)
				break loop
			}
			for _, window := range buffer.Push(chunk) {
				features, err := e.config.Analyzer.Analyze(ctx, window)
				if err != nil {
					runErr = fmt.Errorf("analyze live audio: %w", err)
					break loop
				}
				state := e.config.Tracker.Update(features)
				if e.config.Observer != nil {
					e.config.Observer.Observe(state)
				}
				if events := e.config.Generator.Generate(state); len(events) > 0 {
					offerLatestBatch(eventBatches, events)
				}
			}
		}
	}

	close(eventBatches)
	if runErr != nil || ctx.Err() != nil {
		cancel()
		wg.Wait()
	} else {
		// A finite/test source gets a graceful drain. Real microphone sources end
		// through context cancellation and take the branch above.
		wg.Wait()
		cancel()
	}
	return runErr
}

func offerLatestBatch(output chan []timeline.Event, events []timeline.Event) {
	select {
	case output <- events:
		return
	default:
	}
	select {
	case <-output:
	default:
	}
	select {
	case output <- events:
	default:
	}
}
