package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/scheduler"
	"lifx-maestro/internal/timeline"
)

type Options struct {
	DryRun     bool
	Verbose    bool
	Out        io.Writer
	ClockLabel string
}

type Player struct {
	controller devices.DeviceController
	options    Options
}

func NewPlayer(controller devices.DeviceController, options Options) *Player {
	return &Player{controller: controller, options: options}
}

func (p *Player) Play(ctx context.Context, tl *timeline.Timeline) error {
	return p.PlayWithClock(ctx, tl, scheduler.NewMonotonicClock())
}

func (p *Player) PlayWithClock(ctx context.Context, tl *timeline.Timeline, clock scheduler.Clock) error {
	if tl == nil {
		return fmt.Errorf("timeline is required")
	}

	events := make([]scheduler.Event, 0, len(tl.Events))
	for i, event := range tl.Events {
		events = append(events, scheduler.Event{
			TimeMS: event.TimeMS,
			Index:  i,
			Value:  event,
		})
	}

	jobs := make(chan scheduler.Event, len(events))
	errs := make(chan error, len(events))

	var wg sync.WaitGroup
	workerCount := 4
	if len(events) < workerCount {
		workerCount = len(events)
	}
	if workerCount == 0 {
		return nil
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for event := range jobs {
				if err := p.execute(event); err != nil {
					p.logf("[playback] event index=%d error=%v", event.Index, err)
					errs <- err
				}
			}
		}()
	}

	s := scheduler.New(scheduler.Options{
		PollInterval: 2 * time.Millisecond,
		Lookahead:    8 * time.Millisecond,
		Logf: func(format string, args ...interface{}) {
			if p.options.Verbose && p.options.Out != nil {
				label := p.options.ClockLabel
				if label == "" {
					label = "clock"
				}
				fmt.Fprintf(p.options.Out, "["+label+" "+format+"\n", args...)
			}
		},
	})

	err := s.RunWithClock(ctx, clock, events, func(ctx context.Context, event scheduler.Event) {
		select {
		case <-ctx.Done():
		case jobs <- event:
		}
	})

	close(jobs)
	wg.Wait()
	close(errs)

	if err != nil {
		return err
	}

	for execErr := range errs {
		if execErr != nil {
			return execErr
		}
	}

	return nil
}

func (p *Player) execute(scheduled scheduler.Event) error {
	event, ok := scheduled.Value.(timeline.Event)
	if !ok {
		return fmt.Errorf("scheduled event %d has unexpected value type", scheduled.Index)
	}

	p.logf("[playback] executing index=%d target=%s action=%s", scheduled.Index, event.Target, event.Action)

	switch event.Action {
	case "power_on":
		return p.controller.PowerOn(event.Target)
	case "power_off":
		return p.controller.PowerOff(event.Target)
	case "set_color":
		params, err := colorParams(event.Params)
		if err != nil {
			return fmt.Errorf("event %d: %w", scheduled.Index, err)
		}
		return p.controller.SetColor(event.Target, params)
	case "set_zone_colors":
		params, err := zoneColorParams(event.Params)
		if err != nil {
			return fmt.Errorf("event %d: %w", scheduled.Index, err)
		}
		return p.controller.SetZoneColors(event.Target, params.zones, params.durationMS)
	case "set_matrix_colors":
		params, err := matrixColorParams(event.Params)
		if err != nil {
			return fmt.Errorf("event %d: %w", scheduled.Index, err)
		}
		return p.controller.SetMatrixColors(event.Target, params.pixels, params.width, params.height, params.durationMS)
	default:
		return fmt.Errorf("event %d: unsupported action %q", scheduled.Index, event.Action)
	}
}

func (p *Player) logf(format string, args ...interface{}) {
	if !p.options.Verbose || p.options.Out == nil {
		return
	}
	fmt.Fprintf(p.options.Out, format+"\n", args...)
}

func colorParams(raw json.RawMessage) (devices.ColorParams, error) {
	var params timeline.SetColorParams
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			return devices.ColorParams{}, fmt.Errorf("decode set_color params: %w", err)
		}
	}

	return devices.ColorParams{
		Hue:        valueOr(params.Hue, 0),
		Saturation: valueOr(params.Saturation, 0),
		Brightness: brightnessOr(params.Brightness, 1),
		Kelvin:     valueOr(params.Kelvin, 3500),
		DurationMS: valueOr(params.DurationMS, 0),
	}, nil
}

type zoneColorCommand struct {
	zones      []devices.ZoneColorParams
	durationMS int64
}

func zoneColorParams(raw json.RawMessage) (zoneColorCommand, error) {
	var params timeline.SetZoneColorsParams
	if err := decodeParams(raw, &params, "set_zone_colors"); err != nil {
		return zoneColorCommand{}, err
	}
	zones := make([]devices.ZoneColorParams, 0, len(params.Zones))
	for _, zone := range params.Zones {
		zones = append(zones, devices.ZoneColorParams{
			Index:      zone.Index,
			Hue:        zone.Color.Hue,
			Saturation: zone.Color.Saturation,
			Brightness: minBrightness(zone.Color.Brightness),
			Kelvin:     zone.Color.Kelvin,
		})
	}
	return zoneColorCommand{zones: zones, durationMS: params.DurationMS}, nil
}

type matrixColorCommand struct {
	pixels     []devices.MatrixColorParams
	width      int
	height     int
	durationMS int64
}

func matrixColorParams(raw json.RawMessage) (matrixColorCommand, error) {
	var params timeline.SetMatrixColorsParams
	if err := decodeParams(raw, &params, "set_matrix_colors"); err != nil {
		return matrixColorCommand{}, err
	}
	pixels := make([]devices.MatrixColorParams, 0, len(params.Pixels))
	for _, pixel := range params.Pixels {
		pixels = append(pixels, devices.MatrixColorParams{
			X:          pixel.X,
			Y:          pixel.Y,
			Hue:        pixel.Color.Hue,
			Saturation: pixel.Color.Saturation,
			Brightness: minBrightness(pixel.Color.Brightness),
			Kelvin:     pixel.Color.Kelvin,
		})
	}
	return matrixColorCommand{pixels: pixels, width: params.Width, height: params.Height, durationMS: params.DurationMS}, nil
}

func decodeParams(raw json.RawMessage, value interface{}, action string) error {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode %s params: %w", action, err)
	}
	return nil
}

func valueOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}

func brightnessOr(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return minBrightness(*value)
}

func minBrightness(value float64) float64 {
	if value <= 0 {
		return 1
	}
	return value
}

func FormatOffset(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	totalMS := d.Milliseconds()
	minutes := totalMS / 60000
	seconds := (totalMS / 1000) % 60
	millis := totalMS % 1000
	return fmt.Sprintf("%02d:%02d.%03d", minutes, seconds, millis)
}
