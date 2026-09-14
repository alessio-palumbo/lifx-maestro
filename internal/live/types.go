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
	Onset           bool
	TempoBPM        float64
	TempoConfidence float64
	TempoCandidates []TempoCandidate
	Beat            bool
}

type TempoCandidate struct {
	BPM        float64
	Confidence float64
	Strength   float64
}

type DynamicsLevel string

const (
	DynamicsCalm      DynamicsLevel = "calm"
	DynamicsBalanced  DynamicsLevel = "balanced"
	DynamicsEnergetic DynamicsLevel = "energetic"
)

type State struct {
	At              time.Duration
	InputDB         float64
	NoiseFloorDB    float64
	MarginDB        float64
	Energy          float64
	Presence        float64
	Low             float64
	Mid             float64
	High            float64
	Trend           float64
	Onset           bool
	Beat            bool
	TempoBPM        float64
	TempoConfidence float64
	Active          bool
	Sustained       bool
	Activity        float64
	Intensity       float64
	Dynamics        DynamicsLevel
	Novelty         float64
	SectionChange   bool
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

type OutputActivity struct {
	At              time.Duration
	GeneratedEvents int
	DroppedEvents   int
}

type OutputObserver interface {
	ObserveOutput(OutputActivity)
}

type ObserverFunc func(State)

func (f ObserverFunc) Observe(state State) {
	f(state)
}
