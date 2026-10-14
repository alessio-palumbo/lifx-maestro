// Package live generates reactive logical lighting frames from streaming PCM.
// Hosts own device discovery, renderers, power, and state restoration.
package live

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/internal/devices"
	"github.com/alessio-palumbo/lifx-maestro/internal/generation"
	core "github.com/alessio-palumbo/lifx-maestro/internal/live"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type PCMChunk = core.PCMChunk
type PCMWindow = core.PCMWindow
type Features = core.Features
type TempoCandidate = core.TempoCandidate
type State = core.State
type DynamicsLevel = core.DynamicsLevel
type AudioSource = core.AudioSource
type Analyzer = core.Analyzer
type Observer = core.Observer
type ObserverFunc = core.ObserverFunc
type OutputActivity = core.OutputActivity
type OutputObserver = core.OutputObserver
type TrackerConfig = core.TrackerConfig
type Sensitivity = core.Sensitivity

const (
	DynamicsCalm          = core.DynamicsCalm
	DynamicsBalanced      = core.DynamicsBalanced
	DynamicsEnergetic     = core.DynamicsEnergetic
	SensitivityLow        = core.SensitivityLow
	SensitivityNormal     = core.SensitivityNormal
	SensitivityHigh       = core.SensitivityHigh
	DefaultAnalysisWindow = core.DefaultAnalysisWindow
	DefaultAnalysisHop    = core.DefaultAnalysisHop
)

func DefaultTrackerConfig() TrackerConfig { return core.DefaultTrackerConfig() }
func TrackerConfigForSensitivity(value string) (TrackerConfig, error) {
	return core.TrackerConfigForSensitivity(value)
}

// Target identifies a host-owned light and its complete logical surface.
// SurfaceFromDevice supplies chain layout, logical bounds, and hidden cells.
type Target struct {
	ID      string
	Surface lifxdevice.Surface
}

// TargetFrame is an entire logical surface, with no orientation or physical
// adaptation applied. Pass Frame once to a lifxlan-go logical frame renderer;
// it handles every matrix chain, hidden cell, and orientation. Target is the
// opaque ID supplied by the host. Batch slices and colors belong to the sink.
type TargetFrame = core.TargetFrame
type FrameBatch = []TargetFrame

// Sink sends one batch synchronously, honoring cancellation. Write must not
// return while its own asynchronous output is still running. A failure stops
// the engine and is returned by Run. The engine retains at most one pending
// batch, replacing older batches while Write is busy.
type Sink interface {
	Write(context.Context, FrameBatch) error
}

type SinkFunc func(context.Context, FrameBatch) error

func (f SinkFunc) Write(ctx context.Context, batch FrameBatch) error { return f(ctx, batch) }

type Config struct {
	Source   AudioSource
	Analyzer Analyzer
	Targets  []Target
	Sink     Sink
	Observer Observer
	Style    string
	// Dynamics is auto (the default), calm, balanced, or energetic.
	Dynamics       string
	Tracker        TrackerConfig
	WindowDuration time.Duration
	HopDuration    time.Duration
}

// Engine is a single-use run. The host owns the injected source, analyzer, and
// sink; Engine never closes them. Run joins capture and output before returning,
// so a host may cancel, wait for Run, then restore lights. Normal cancellation
// returns nil; source, analyzer, and sink failures are returned. Source.Run and
// Analyzer.Analyze must honor context cancellation. Source must release native
// capture resources before Run returns and must not close the output channel.
// PCM sample slices transfer to the engine and must not be reused by the source.
// Observers run synchronously on the analysis loop and should return promptly.
type Engine struct {
	engine    *core.Engine
	generator *core.Generator
	started   atomic.Bool
}

func New(config Config) (*Engine, error) {
	if config.Sink == nil {
		return nil, fmt.Errorf("live frame sink is required")
	}
	if len(config.Targets) == 0 {
		return nil, fmt.Errorf("live requires explicit targets")
	}
	infos := make([]devices.DeviceInfo, 0, len(config.Targets))
	seen := make(map[string]bool)
	for _, target := range config.Targets {
		if target.ID == "" || seen[target.ID] {
			return nil, fmt.Errorf("live target IDs must be nonempty and unique")
		}
		seen[target.ID] = true
		surface := target.Surface
		var kind devices.DeviceKind
		switch surface.LightType {
		case lifxdevice.LightTypeSingleZone:
			kind = devices.DeviceKindSingleZone
		case lifxdevice.LightTypeMultiZone:
			kind = devices.DeviceKindMultiZone
		case lifxdevice.LightTypeMatrix:
			kind = devices.DeviceKindMatrix
		default:
			return nil, fmt.Errorf("unsupported light type for target %q", target.ID)
		}
		if surface.Width <= 0 || surface.Height <= 0 || surface.Zones <= 0 {
			return nil, fmt.Errorf("target %q requires a populated logical surface", target.ID)
		}
		// Copy chain geometry: the host may refresh its inventory during a run.
		if surface.Matrix != nil {
			matrix := *surface.Matrix
			matrix.Chains = append([]lifxdevice.MatrixChain(nil), matrix.Chains...)
			for i := range matrix.Chains {
				matrix.Chains[i].Rows = append([]lifxdevice.MatrixRow(nil), matrix.Chains[i].Rows...)
				for j := range matrix.Chains[i].Rows {
					row := &matrix.Chains[i].Rows[j]
					row.HiddenCols = append([]int(nil), row.HiddenCols...)
				}
			}
			surface.Matrix = &matrix
		}
		infos = append(infos, devices.DeviceInfo{ID: target.ID, Capabilities: devices.DeviceCapabilities{
			Kind: kind, Surface: surface, ZoneCount: surface.Zones,
			MatrixWidth: surface.Width, MatrixHeight: surface.Height,
		}})
	}
	generator, err := core.NewGenerator(core.GeneratorConfig{
		Style: config.Style, Intensity: generation.DynamicsOverride(config.Dynamics), Devices: infos, LogicalFrames: true,
	})
	if err != nil {
		return nil, err
	}
	tracker := config.Tracker
	if tracker == (TrackerConfig{}) {
		tracker = DefaultTrackerConfig()
	}
	engine, err := core.NewEngine(core.EngineConfig{
		Source: config.Source, Analyzer: config.Analyzer, Tracker: core.NewStateTracker(tracker),
		Generator: generator, FrameSink: sinkAdapter{config.Sink}, Observer: config.Observer,
		WindowDuration: config.WindowDuration, HopDuration: config.HopDuration, BorrowAnalyzer: true,
	})
	if err != nil {
		return nil, err
	}
	return &Engine{engine: engine, generator: generator}, nil
}

func (e *Engine) Run(ctx context.Context) error {
	if !e.started.CompareAndSwap(false, true) {
		return fmt.Errorf("live engine has already run")
	}
	return e.engine.Run(ctx)
}

func (e *Engine) SetStyle(name string) error { return e.generator.SetStyle(name) }
func (e *Engine) SetDynamics(value string) error {
	return e.generator.SetIntensity(generation.DynamicsOverride(value))
}

type sinkAdapter struct{ sink Sink }

func (s sinkAdapter) Run(ctx context.Context, input <-chan []TargetFrame) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case batch, ok := <-input:
			if !ok {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.sink.Write(ctx, batch); err != nil {
				return err
			}
		}
	}
}
