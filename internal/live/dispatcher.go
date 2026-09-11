package live

import (
	"context"
	"sync"

	"lifx-maestro/internal/timeline"
)

type ImmediateExecutor interface {
	ExecuteEvent(index int, event timeline.Event) error
}

type DispatchErrorFunc func(event timeline.Event, err error)

// Dispatcher serializes writes to each target while allowing separate targets
// to progress concurrently. A target's one-slot queue is replaced when full so
// delayed LAN writes cannot make Live replay stale states seconds later.
type Dispatcher struct {
	executor ImmediateExecutor
	onError  DispatchErrorFunc
}

func NewDispatcher(executor ImmediateExecutor, onError DispatchErrorFunc) *Dispatcher {
	return &Dispatcher{executor: executor, onError: onError}
}

func (d *Dispatcher) Run(ctx context.Context, input <-chan []timeline.Event) error {
	workers := make(map[string]chan timeline.Event)
	var wg sync.WaitGroup
	defer func() {
		for _, queue := range workers {
			close(queue)
		}
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case events, ok := <-input:
			if !ok {
				return nil
			}
			for _, event := range events {
				queue, ok := workers[event.Target]
				if !ok {
					queue = make(chan timeline.Event, 1)
					workers[event.Target] = queue
					wg.Add(1)
					go d.runTarget(ctx, queue, &wg)
				}
				enqueueLatest(queue, event)
			}
		}
	}
}

func (d *Dispatcher) runTarget(ctx context.Context, queue <-chan timeline.Event, wg *sync.WaitGroup) {
	defer wg.Done()
	index := 0
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-queue:
			if !ok {
				return
			}
			if err := d.executor.ExecuteEvent(index, event); err != nil && d.onError != nil {
				d.onError(event, err)
			}
			index++
		}
	}
}

func enqueueLatest(queue chan timeline.Event, event timeline.Event) {
	select {
	case queue <- event:
		return
	default:
	}

	select {
	case <-queue:
	default:
	}
	select {
	case queue <- event:
	default:
	}
}
