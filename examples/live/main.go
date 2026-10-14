// This hardware-free example demonstrates the public API and host-owned output.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/live"
	"github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type source struct{}

func (source) Name() string { return "example PCM" }
func (source) Run(ctx context.Context, out chan<- live.PCMChunk) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			select {
			case out <- live.PCMChunk{SampleRate: 1000, Samples: make([]float32, 100)}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

type analyzer struct{}

func (analyzer) Analyze(_ context.Context, w live.PCMWindow) (live.Features, error) {
	return live.Features{At: w.End, RMSDB: -15, LowDB: -17, MidDB: -20, HighDB: -25, Onset: true, OnsetStrength: 0.8}, nil
}
func (analyzer) Close() error { return nil }

func main() {
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	a := analyzer{}
	defer a.Close()
	engine, err := live.New(live.Config{
		Source: source{}, Analyzer: a,
		Targets: []live.Target{{ID: "lamp", Surface: device.Surface{LightType: device.LightTypeSingleZone, Width: 1, Height: 1, Zones: 1}}},
		Sink: live.SinkFunc(func(ctx context.Context, batch live.FrameBatch) error {
			for _, output := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				// A real host calls its lifxlan-go renderer.RenderFrame(ctx, output.Frame).
				fmt.Printf("%s: %dx%d logical frame\n", output.Target, output.Frame.Width, output.Frame.Height)
			}
			return nil
		}),
	})
	if err == nil {
		err = engine.Run(ctx)
	}
	if err != nil {
		panic(err)
	}
}
