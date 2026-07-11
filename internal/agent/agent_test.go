package agent

import (
	"context"
	"testing"
	"time"
)

// applyDesired runs the update in a goroutine, so tests wait on a channel.

func TestApplyDesiredTriggersUpdate(t *testing.T) {
	got := make(chan string, 1)
	a := &Agent{version: "0.7.0", update: func(v string) error { got <- v; return nil }}
	a.lock.Store(LockNone)

	a.applyDesired(context.Background(), Desired{PanelVersion: "0.8.0"})

	select {
	case v := <-got:
		if v != "0.8.0" {
			t.Fatalf("update called with %q, want 0.8.0", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update was not triggered for a newer desired version")
	}
}

func TestApplyDesiredSkipsSameVersion(t *testing.T) {
	called := make(chan string, 1)
	a := &Agent{version: "0.8.0", update: func(v string) error { called <- v; return nil }}
	a.lock.Store(LockNone)

	a.applyDesired(context.Background(), Desired{PanelVersion: "0.8.0"})

	select {
	case <-called:
		t.Fatal("update should not run when desired version equals the running version")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestApplyDesiredUpdatesOnlyOnce(t *testing.T) {
	count := 0
	done := make(chan struct{}, 4)
	a := &Agent{version: "0.7.0", update: func(v string) error { count++; done <- struct{}{}; return nil }}
	a.lock.Store(LockNone)

	// Three syncs in a row all report the same desired version.
	for i := 0; i < 3; i++ {
		a.applyDesired(context.Background(), Desired{PanelVersion: "0.8.0"})
	}
	<-done
	time.Sleep(200 * time.Millisecond)
	if count != 1 {
		t.Fatalf("update ran %d times, want 1 (must not loop)", count)
	}
}

func TestLockLevelTracked(t *testing.T) {
	a := &Agent{version: "0.7.0"}
	a.lock.Store(LockNone)
	if a.Lock() != LockNone {
		t.Fatalf("initial lock = %q", a.Lock())
	}
	// grace does not touch the manager, safe with a nil manager.
	a.applyDesired(context.Background(), Desired{Lock: LockGrace})
	if a.Lock() != LockGrace {
		t.Fatalf("lock = %q, want grace", a.Lock())
	}
}
