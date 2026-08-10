package main

import (
	"testing"
	"time"
)

func TestStartProfileJoinsPendingStart(t *testing.T) {
	id := "pending-profile"
	want := &runningProfile{}
	pending := &profileStartState{done: make(chan struct{})}
	pendingClosed := false

	startMu.Lock()
	startingProfiles[id] = pending
	startMu.Unlock()
	defer func() {
		startMu.Lock()
		delete(startingProfiles, id)
		if !pendingClosed {
			close(pending.done)
		}
		startMu.Unlock()
	}()

	type result struct {
		rp      *runningProfile
		started bool
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		rp, started, err := startProfile(id)
		resultCh <- result{rp: rp, started: started, err: err}
	}()

	select {
	case <-resultCh:
		t.Fatal("startProfile() returned before the pending start completed")
	case <-time.After(20 * time.Millisecond):
	}

	startMu.Lock()
	pending.rp = want
	delete(startingProfiles, id)
	close(pending.done)
	pendingClosed = true
	startMu.Unlock()

	got := <-resultCh
	if got.err != nil || got.rp != want || got.started {
		t.Fatalf("startProfile() = (%p, %v, %v), want (%p, false, nil)", got.rp, got.started, got.err, want)
	}
}
