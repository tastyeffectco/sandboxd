package api

import (
	"sync"
	"sync/atomic"
	"testing"
)

// One caller at a time per agent; different agents don't wait on each other.
func TestAgentLocksSerialisePerAgent(t *testing.T) {
	var l agentLocks
	var inside, most atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer l.lock("a")()
			n := inside.Add(1)
			for {
				m := most.Load()
				if n <= m || most.CompareAndSwap(m, n) {
					break
				}
			}
			inside.Add(-1)
		}()
	}
	wg.Wait()
	if most.Load() != 1 {
		t.Fatalf("%d callers made agent a at once", most.Load())
	}
	unlockA := l.lock("a")
	done := make(chan struct{})
	go func() { l.lock("b")(); close(done) }()
	<-done // would hang if b waited on a
	unlockA()
}
