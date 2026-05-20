package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunDispatchesSortedEvents(t *testing.T) {
	events := []Event{
		{TimeMS: 4, Index: 1, Value: "second"},
		{TimeMS: 0, Index: 0, Value: "first"},
		{TimeMS: 4, Index: 2, Value: "third"},
	}

	var got []string
	s := New(Options{
		PollInterval: time.Millisecond,
		Lookahead:    2 * time.Millisecond,
	})

	if err := s.Run(context.Background(), events, func(ctx context.Context, event Event) {
		got = append(got, event.Value.(string))
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunWithClockFollowsInjectedClock(t *testing.T) {
	clock := &testClock{}
	events := []Event{
		{TimeMS: 10, Index: 0, Value: "beat"},
	}

	dispatched := make(chan Event, 1)
	s := New(Options{
		PollInterval: time.Millisecond,
		Lookahead:    time.Millisecond,
	})

	errs := make(chan error, 1)
	go func() {
		errs <- s.RunWithClock(context.Background(), clock, events, func(ctx context.Context, event Event) {
			dispatched <- event
		})
	}()

	select {
	case <-dispatched:
		t.Fatal("event dispatched before clock reached event time")
	case <-time.After(3 * time.Millisecond):
	}

	clock.set(10 * time.Millisecond)

	select {
	case event := <-dispatched:
		if event.Value.(string) != "beat" {
			t.Fatalf("value = %v, want beat", event.Value)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("event was not dispatched")
	}

	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

type testClock struct {
	position atomic.Int64
}

func (t *testClock) Position() time.Duration {
	return time.Duration(t.position.Load())
}

func (t *testClock) set(position time.Duration) {
	t.position.Store(int64(position))
}
