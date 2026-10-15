package live

import (
	"context"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

func TestMasterBrightnessOutput(t *testing.T) {
	for _, geometry := range [][2]int{{1, 1}, {16, 1}, {16, 8}} {
		original := effects.Frame{Width: geometry[0], Height: geometry[1], Duration: 150 * time.Millisecond}
		for i := 0; i < original.Width*original.Height; i++ {
			original.Colors = append(original.Colors, device.Color{
				Hue: float64(i), Saturation: 70, Brightness: float64(20 + i%60), Kelvin: 3500,
			})
		}
		master := &atomic.Uint64{}
		engine := &Engine{master: master}
		engine.SetMasterBrightness(1)
		input := make(chan []TargetFrame, 5)
		for i := 0; i < cap(input); i++ {
			input <- []TargetFrame{{Target: "light", Frame: original}}
		}
		close(input)
		scales := []float64{1, .5, .25, 0, 1}
		var retained FrameBatch
		writes := 0
		sink := sinkAdapter{master: master, sink: SinkFunc(func(_ context.Context, batch FrameBatch) error {
			scale := scales[writes]
			if batch[0].Target != "light" || batch[0].Frame.Width != original.Width || batch[0].Frame.Height != original.Height || batch[0].Frame.Duration != original.Duration {
				t.Fatal("output geometry/target changed")
			}
			for i, color := range batch[0].Frame.Colors {
				want := original.Colors[i]
				want.Brightness *= scale
				if scale == 0 {
					want.Brightness = 1
				}
				if color != want {
					t.Fatalf("scale %v color %d: got %+v want %+v", scale, i, color, want)
				}
			}
			if writes == 1 {
				retained = batch
			}
			writes++
			if writes < len(scales) {
				engine.SetMasterBrightness(scales[writes])
			}
			return nil
		})}
		if err := sink.Run(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if writes != len(scales) {
			t.Fatalf("writes = %d", writes)
		}
		if retained[0].Frame.Colors[0].Brightness != 10 || original.Colors[0].Brightness != 20 {
			t.Fatal("scaling mutated original or previously delivered colors")
		}
	}
}

func TestMasterBrightnessClamp(t *testing.T) {
	master := &atomic.Uint64{}
	for _, test := range []struct{ in, want float64 }{
		{-1, 0}, {2, 1}, {math.NaN(), 1}, {math.Inf(1), 1}, {math.Inf(-1), 0}, {.4, .4},
	} {
		storeMasterBrightness(master, test.in)
		if got := math.Float64frombits(master.Load()); got != test.want {
			t.Fatalf("got %v want %v", got, test.want)
		}
	}
}

func TestFullBrightnessPreservesZero(t *testing.T) {
	master := &atomic.Uint64{}
	storeMasterBrightness(master, 1)
	want := FrameBatch{{Frame: effects.Frame{Width: 1, Height: 1, Colors: []device.Color{{Brightness: 0}}}}}
	input := make(chan []TargetFrame, 1)
	input <- want
	close(input)
	sink := sinkAdapter{master: master, sink: SinkFunc(func(_ context.Context, got FrameBatch) error {
		if !reflect.DeepEqual(got, want) {
			t.Fatal("full scale changed original frame")
		}
		return nil
	})}
	if err := sink.Run(context.Background(), input); err != nil {
		t.Fatal(err)
	}
}
