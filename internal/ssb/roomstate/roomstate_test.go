package roomstate

import (
	"bytes"
	"sync"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func stressFeedRef(seed byte) refs.FeedRef {
	return *refs.MustNewFeedRef(bytes.Repeat([]byte{seed}, 32), refs.RefAlgoFeedSSB1)
}

func TestManagerAttendantSubscribeCancelStress(t *testing.T) {
	manager := NewManager()

	const rounds = 10
	const subscriberWorkers = 8
	const emitterWorkers = 8
	const iterations = 300

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup

		for worker := 0; worker < subscriberWorkers; worker++ {
			wg.Add(1)
			go func(offset int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					_, ch, cancel := manager.SubscribeAttendants()
					// Drain opportunistically to exercise active and idle subscribers.
					if (offset+i)%2 == 0 {
						select {
						case <-ch:
						default:
						}
					}
					cancel()
				}
			}(worker)
		}

		for worker := 0; worker < emitterWorkers; worker++ {
			wg.Add(1)
			go func(offset int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					id := stressFeedRef(byte(((offset * iterations) + i)%240 + 1))
					manager.AddAttendant(id, "127.0.0.1:8008")
					manager.RemoveAttendant(id)
				}
			}(worker)
		}

		wg.Wait()
	}

	if got := len(manager.Attendants()); got != 0 {
		t.Fatalf("expected 0 attendants after stress run, got %d", got)
	}
}

func TestManagerEndpointSubscribeCancelStress(t *testing.T) {
	manager := NewManager()

	const rounds = 10
	const subscriberWorkers = 8
	const emitterWorkers = 8
	const iterations = 300

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup

		for worker := 0; worker < subscriberWorkers; worker++ {
			wg.Add(1)
			go func(offset int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					_, ch, cancel := manager.SubscribeEndpoints()
					// Drain opportunistically to exercise active and idle subscribers.
					if (offset+i)%2 == 0 {
						select {
						case <-ch:
						default:
						}
					}
					cancel()
				}
			}(worker)
		}

		for worker := 0; worker < emitterWorkers; worker++ {
			wg.Add(1)
			go func(offset int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					id := stressFeedRef(byte(((offset * iterations) + i)%240 + 1))
					manager.AddPeer(id, "127.0.0.1:8008")
					manager.RemovePeer(id)
				}
			}(worker)
		}

		wg.Wait()
	}

	if got := manager.PeerCount(); got != 0 {
		t.Fatalf("expected 0 peers after stress run, got %d", got)
	}
}
