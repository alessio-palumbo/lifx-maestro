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

type Clock interface {
	Position() time.Duration
}

type Options struct {
	PollInterval time.Duration
	Lookahead    time.Duration
	Logf         func(format string, args ...interface{})
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
	return s.RunWithClock(ctx, NewMonotonicClock(), events, dispatch)
}

func (s *Scheduler) RunWithClock(ctx context.Context, clock Clock, events []Event, dispatch DispatchFunc) error {
	if dispatch == nil {
		return fmt.Errorf("dispatch function is required")
	}
	if clock == nil {
		return fmt.Errorf("clock is required")
	}

	events = sortedEvents(events)
	next := 0
	dispatched := make(map[int]bool, len(events))
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()

	for next < len(events) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		now := clock.Position()
		horizon := now + s.options.Lookahead

		for next < len(events) && msToDuration(events[next].TimeMS) <= horizon {
			event := events[next]
			eventTime := msToDuration(event.TimeMS)

			if eventTime > now {
				break
			}
			if dispatched[event.Index] {
				next++
				continue
			}

			dispatched[event.Index] = true
			if s.options.Logf != nil {
				s.options.Logf("%s] dispatching event index=%d latency=%s", FormatOffset(now), event.Index, now-eventTime)
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

type MonotonicClock struct {
	start time.Time
}

func NewMonotonicClock() *MonotonicClock {
	return &MonotonicClock{start: time.Now()}
}

func (m *MonotonicClock) Position() time.Duration {
	return time.Since(m.start)
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
