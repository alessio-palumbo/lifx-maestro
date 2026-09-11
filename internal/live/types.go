package live

import (
	"context"
	"time"

	"lifx-maestro/internal/timeline"
)

type PCMChunk struct {
	Samples    []float32
	SampleRate int
	CapturedAt time.Duration
}

type PCMWindow struct {
	Samples    []float32
	SampleRate int
	Start      time.Duration
	End        time.Duration
}

type Features struct {
	At              time.Duration
	RMSDB           float64
	LowDB           float64
	MidDB           float64
	HighDB          float64
	OnsetStrength   float64
	TempoBPM        float64
	TempoConfidence float64
	Beat            bool
}

type State struct {
	At              time.Duration
	NoiseFloorDB    float64
	Energy          float64
	Low             float64
	Mid             float64
	High            float64
	Trend           float64
	Onset           bool
	Beat            bool
	TempoBPM        float64
	TempoConfidence float64
	Active          bool
}

type AudioSource interface {
	Name() string
	Run(ctx context.Context, output chan<- PCMChunk) error
}

type Analyzer interface {
	Analyze(ctx context.Context, window PCMWindow) (Features, error)
	Close() error
}

type EventSink interface {
	Run(ctx context.Context, input <-chan []timeline.Event) error
}

type Observer interface {
	Observe(State)
}

type ObserverFunc func(State)

func (f ObserverFunc) Observe(state State) {
	f(state)
}
