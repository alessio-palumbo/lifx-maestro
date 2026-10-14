package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/internal/timeline"
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
	FrameSink FrameSink
	Observer  Observer
	// BorrowAnalyzer leaves Close to the caller. Session uses this so it closes
	// once even when capture or power-on fails before Engine.Run.
	BorrowAnalyzer bool

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
	if config.Sink == nil && config.FrameSink == nil {
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
	if e.config.FrameSink != nil {
		return runEngine(e, ctx, func(state State) []TargetFrame { return e.config.Generator.GenerateDecision(state).Frames }, e.config.FrameSink.Run)
	}
	return runEngine(e, ctx, e.config.Generator.Generate, e.config.Sink.Run)
}

func runEngine[T any](e *Engine, ctx context.Context, generate func(State) []T, sink func(context.Context, <-chan []T) error) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !e.config.BorrowAnalyzer {
		defer func() { result = errors.Join(result, e.config.Analyzer.Close()) }()
	}

	chunks := make(chan PCMChunk, 8)
	eventBatches := make(chan []T, 1)
	sourceDone := make(chan error, 1)
	sinkDone := make(chan error, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(chunks)
		err := e.config.Source.Run(ctx, chunks)
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			err = nil
		}
		sourceDone <- err
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := sink(ctx, eventBatches)
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			err = nil
		}
		sinkDone <- err
		cancel()
	}()

	var buffer *WindowBuffer
	var runErr error
	var sinkErr error
	sinkFinished := false
	sourceFinished := false
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-sinkDone:
			sinkFinished = true
			if err != nil {
				runErr = fmt.Errorf("dispatch live events: %w", err)
			}
			break loop
		case chunk, ok := <-chunks:
			if !ok {
				if err := <-sourceDone; err != nil {
					runErr = fmt.Errorf("capture audio: %w", err)
				}
				sourceFinished = true
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
					if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
						runErr = fmt.Errorf("analyze live audio: %w", err)
					}
					break loop
				}
				state := e.config.Tracker.Update(features)
				events := generate(state)
				dropped := 0
				if len(events) > 0 {
					dropped = offerLatest(eventBatches, events)
				}
				if observer, ok := e.config.Observer.(OutputObserver); ok {
					observer.ObserveOutput(OutputActivity{At: state.At, GeneratedEvents: len(events), DroppedEvents: dropped})
				}
				if e.config.Observer != nil {
					e.config.Observer.Observe(state)
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
	if !sinkFinished {
		sinkErr = <-sinkDone
	}
	if !sourceFinished {
		if err := <-sourceDone; err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("capture audio: %w", err))
		}
	}
	if sinkErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("dispatch live events: %w", sinkErr))
	}
	return runErr
}

func offerLatestBatch(output chan []timeline.Event, events []timeline.Event) int {
	return offerLatest(output, events)
}

func offerLatest[T any](output chan []T, events []T) int {
	select {
	case output <- events:
		return 0
	default:
	}
	dropped := 0
	select {
	case previous := <-output:
		dropped = len(previous)
	default:
	}
	select {
	case output <- events:
	default:
	}
	return dropped
}
