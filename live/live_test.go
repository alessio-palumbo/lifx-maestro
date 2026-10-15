package live_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/live"
	"github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

type source struct {
	run func(context.Context, chan<- live.PCMChunk) error
}

func (source) Name() string                                              { return "injected PCM" }
func (s source) Run(ctx context.Context, out chan<- live.PCMChunk) error { return s.run(ctx, out) }

type analyzer struct {
	err    error
	closes atomic.Int32
}

func (a *analyzer) Analyze(ctx context.Context, w live.PCMWindow) (live.Features, error) {
	if err := ctx.Err(); err != nil {
		return live.Features{}, err
	}
	return live.Features{At: w.End, RMSDB: -12, LowDB: -14, MidDB: -18, HighDB: -20, Onset: true, OnsetStrength: 0.8}, a.err
}
func (a *analyzer) Close() error { a.closes.Add(1); return nil }

func chunk() live.PCMChunk { return live.PCMChunk{SampleRate: 100, Samples: make([]float32, 50)} }
func finite(err error) source {
	return source{func(ctx context.Context, out chan<- live.PCMChunk) error {
		select {
		case out <- chunk():
		case <-ctx.Done():
			return ctx.Err()
		}
		return err
	}}
}
func target() live.Target {
	return live.Target{ID: "host-lamp", Surface: device.Surface{LightType: device.LightTypeSingleZone, Width: 1, Height: 1, Zones: 1}}
}
func config(a live.Analyzer, s live.AudioSource, sink live.Sink) live.Config {
	return live.Config{Analyzer: a, Source: s, Targets: []live.Target{target()}, Sink: sink}
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for component")
	}
}

func TestConfiguredMasterBrightness(t *testing.T) {
	run := func(scale *float64) live.FrameBatch {
		t.Helper()
		var result live.FrameBatch
		cfg := config(&analyzer{}, finite(nil), live.SinkFunc(func(_ context.Context, batch live.FrameBatch) error {
			result = batch
			return nil
		}))
		cfg.MasterBrightness = scale
		engine, err := live.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(result) == 0 {
			t.Fatal("no output")
		}
		return result
	}
	full, zero := 1.0, 0.0
	if !reflect.DeepEqual(run(nil), run(&full)) {
		t.Fatal("default is not full brightness")
	}
	for _, output := range run(&zero) {
		for _, color := range output.Frame.Colors {
			if color.Brightness != 1 {
				t.Fatalf("explicit zero brightness = %v", color.Brightness)
			}
		}
	}
}

func TestLogicalBatchPreservesAllMatrixChains(t *testing.T) {
	a := &analyzer{}
	rows := make([]device.MatrixRow, 8)
	for i := range rows {
		rows[i].Cols = 8
	}
	var batch live.FrameBatch
	cfg := config(a, finite(nil), live.SinkFunc(func(_ context.Context, b live.FrameBatch) error { batch = b; return nil }))
	cfg.Targets = []live.Target{{ID: "matrix", Surface: device.Surface{
		LightType: device.LightTypeMatrix, Width: 16, Height: 8, Zones: 128,
		Matrix: &device.MatrixSurface{Chains: []device.MatrixChain{
			{Index: 0, Bounds: device.Rect{Width: 8, Height: 8}, SendWidth: 8, Rows: rows, Orientation: device.OrientationRightSideUp},
			{Index: 1, Bounds: device.Rect{X: 8, Width: 8, Height: 8}, SendWidth: 8, Rows: rows, Orientation: device.OrientationUpsideDown},
		}},
	}}}
	engine, err := live.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].Target != "matrix" {
		t.Fatalf("batch = %+v", batch)
	}
	f := batch[0].Frame
	if f.Width != 16 || f.Height != 8 || len(f.Colors) != 128 {
		t.Fatalf("incomplete logical surface: %+v", f)
	}
	physical, err := effects.AdaptFrameToSurface(f, cfg.Targets[0].Surface, effects.AdaptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(physical) != 2 || physical[1].ChainIndex != 1 || physical[1].Orientation != device.OrientationUpsideDown {
		t.Fatalf("lost matrix chain geometry: %+v", physical)
	}
	// Logical cells are sliced by the host adaptation once, with orientation
	// still available for packet rendering instead of already rotated twice.
	if physical[1].Colors[0] != f.Colors[8] {
		t.Fatal("frame was adapted or oriented before reaching the host")
	}
	if a.closes.Load() != 0 {
		t.Fatal("engine closed the host analyzer")
	}
	if err := engine.Run(context.Background()); err == nil {
		t.Fatal("single-use engine ran twice")
	}
}

func TestFailuresAreSurfaced(t *testing.T) {
	want := errors.New("injected failure")
	for _, stage := range []string{"source", "analyzer", "sink"} {
		t.Run(stage, func(t *testing.T) {
			a := &analyzer{}
			s := finite(nil)
			sink := live.SinkFunc(func(context.Context, live.FrameBatch) error { return nil })
			switch stage {
			case "source":
				s = finite(want)
			case "analyzer":
				a.err = want
			case "sink":
				sink = func(context.Context, live.FrameBatch) error { return want }
			}
			e, err := live.New(config(a, s, sink))
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Run(context.Background()); !errors.Is(err, want) {
				t.Fatalf("Run = %v, want %v", err, want)
			}
			if a.closes.Load() != 0 {
				t.Fatal("host analyzer closed")
			}
		})
	}
}

func TestCancellationQuiescesOutputAndCapture(t *testing.T) {
	entered, exited, captureExited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s := source{func(ctx context.Context, out chan<- live.PCMChunk) error {
		defer close(captureExited)
		select {
		case out <- chunk():
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	e, err := live.New(config(&analyzer{}, s, live.SinkFunc(func(ctx context.Context, _ live.FrameBatch) error {
		close(entered)
		<-ctx.Done()
		defer close(exited)
		return ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	await(t, entered)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
	select {
	case <-exited:
	default:
		t.Fatal("output still running after Run returned")
	}
	select {
	case <-captureExited:
	default:
		t.Fatal("capture still running after Run returned")
	}
}

func TestSlowSinkKeepsOnlyLatestPendingBatch(t *testing.T) {
	entered, release, analyzed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var writes atomic.Int32
	var drops atomic.Int32
	s := source{func(ctx context.Context, out chan<- live.PCMChunk) error {
		select {
		case out <- chunk():
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-entered:
		case <-ctx.Done():
			return ctx.Err()
		}
		for range 30 {
			select {
			case out <- chunk():
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	cfg := config(&analyzer{}, s, live.SinkFunc(func(ctx context.Context, _ live.FrameBatch) error {
		if writes.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}))
	cfg.Observer = &queueObserver{analyzed: analyzed, drops: &drops}
	e, err := live.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	await(t, analyzed)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not drain")
	}
	if writes.Load() != 2 {
		t.Fatalf("writes = %d; stale batches were queued", writes.Load())
	}
	if drops.Load() == 0 {
		t.Fatal("queue replacements were not observed")
	}
}

type queueObserver struct {
	analyzed chan struct{}
	drops    *atomic.Int32
}

func (o *queueObserver) Observe(s live.State) {
	if s.At == 15500*time.Millisecond {
		close(o.analyzed)
	}
}
func (o *queueObserver) ObserveOutput(a live.OutputActivity) { o.drops.Add(int32(a.DroppedEvents)) }

type blockingAnalyzer struct{ entered chan struct{} }

func (a blockingAnalyzer) Analyze(ctx context.Context, _ live.PCMWindow) (live.Features, error) {
	close(a.entered)
	<-ctx.Done()
	return live.Features{}, ctx.Err()
}
func (blockingAnalyzer) Close() error { return nil }

func TestCancellationInterruptsAnalysis(t *testing.T) {
	entered := make(chan struct{})
	e, err := live.New(config(blockingAnalyzer{entered}, finite(nil), live.SinkFunc(func(context.Context, live.FrameBatch) error {
		t.Error("output after blocked analysis")
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	await(t, entered)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("analysis did not stop")
	}
}
