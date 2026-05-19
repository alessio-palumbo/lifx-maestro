package scheduler

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type Event struct {
	TimeMS int64
	Index  int
	Value  interface{}
}

type DispatchFunc func(context.Context, Event)

type Options struct {
	PollInterval time.Duration
	Lookahead    time.Duration
}

type Scheduler struct {
	options Options
}

func New(options Options) *Scheduler {
	if options.PollInterval <= 0 {
		options.PollInterval = 2 * time.Millisecond
	}
	if options.Lookahead <= 0 {
		options.Lookahead = 8 * time.Millisecond
	}

	return &Scheduler{options: options}
}

func (s *Scheduler) Run(ctx context.Context, events []Event, dispatch DispatchFunc) error {
	if dispatch == nil {
		return fmt.Errorf("dispatch function is required")
	}

	events = sortedEvents(events)
	start := time.Now()
	next := 0
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()

	for next < len(events) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		now := time.Since(start)
		horizon := now + s.options.Lookahead

		for next < len(events) && msToDuration(events[next].TimeMS) <= horizon {
			event := events[next]
			dueAt := start.Add(msToDuration(event.TimeMS))

			if wait := time.Until(dueAt); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}

			dispatch(ctx, event)
			next++
		}
	}

	return nil
}

func msToDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

func sortedEvents(events []Event) []Event {
	sorted := append([]Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TimeMS == sorted[j].TimeMS {
			return sorted[i].Index < sorted[j].Index
		}
		return sorted[i].TimeMS < sorted[j].TimeMS
	})
	return sorted
}
