package playback

import (
	"context"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/scheduler"
	"lifx-maestro/internal/timeline"
)

type Options struct {
	DryRun  bool
	Verbose bool
	Out     io.Writer
}

type Player struct {
	controller devices.DeviceController
	options    Options
}

func NewPlayer(controller devices.DeviceController, options Options) *Player {
	return &Player{controller: controller, options: options}
}

func (p *Player) Play(ctx context.Context, tl *timeline.Timeline) error {
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
					errs <- err
				}
			}
		}()
	}

	s := scheduler.New(scheduler.Options{
		PollInterval: 2 * time.Millisecond,
		Lookahead:    8 * time.Millisecond,
	})

	err := s.Run(ctx, events, func(ctx context.Context, event scheduler.Event) {
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
	default:
		return fmt.Errorf("event %d: unsupported action %q", scheduled.Index, event.Action)
	}
}

func colorParams(raw map[string]interface{}) (devices.ColorParams, error) {
	var params devices.ColorParams
	var err error

	if params.Hue, err = number(raw, "hue", 0); err != nil {
		return params, err
	}
	if params.Saturation, err = number(raw, "saturation", 0); err != nil {
		return params, err
	}
	if params.Brightness, err = number(raw, "brightness", 0); err != nil {
		return params, err
	}

	kelvin, err := number(raw, "kelvin", 3500)
	if err != nil {
		return params, err
	}
	durationMS, err := number(raw, "duration_ms", 0)
	if err != nil {
		return params, err
	}

	params.Kelvin = int(math.Round(kelvin))
	params.DurationMS = int64(math.Round(durationMS))
	return params, nil
}

func number(raw map[string]interface{}, key string, fallback float64) (float64, error) {
	if raw == nil {
		return fallback, nil
	}

	value, ok := raw[key]
	if !ok {
		return fallback, nil
	}

	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case jsonNumber:
		return typed.Float64()
	default:
		return 0, fmt.Errorf("param %q must be numeric", key)
	}
}

type jsonNumber interface {
	Float64() (float64, error)
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
