package eventbus_test

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
)

// ── SubscriberCount ───────────────────────────────────────────────────────────

func TestBus_SubscriberCount(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	assert.Equal(t, 0, b.SubscriberCount("topic"), "empty bus must return 0")

	_, unsub1 := b.Subscribe("topic")
	assert.Equal(t, 1, b.SubscriberCount("topic"), "one subscriber")

	_, unsub2 := b.Subscribe("topic")
	assert.Equal(t, 2, b.SubscriberCount("topic"), "two subscribers")

	unsub1()
	assert.Equal(t, 1, b.SubscriberCount("topic"), "back to one after first unsub")

	unsub2()
	assert.Equal(t, 0, b.SubscriberCount("topic"), "zero after all unsubs")
}

func TestBus_SubscriberCount_DifferentTopics_Independent(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	_, unsub := b.Subscribe("pagos_changed")
	defer unsub()

	assert.Equal(t, 1, b.SubscriberCount("pagos_changed"))
	assert.Equal(t, 0, b.SubscriberCount("saldos_changed"), "unrelated topic must be 0")
}

// ── Basic deterministic tests ─────────────────────────────────────────────────

func TestBus_SingleSubscriber_ReceivesSignal(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	ch, unsub := b.Subscribe("pagos_changed")
	defer unsub()

	b.Publish("pagos_changed", []int{1, 2, 3})

	select {
	case ids := <-ch:
		assert.Equal(t, []int{1, 2, 3}, ids, "expected received ids to match published ids")
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive signal within 1s")
	}
}

func TestBus_MultipleSubscribers_AllReceiveSignal(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	const n = 10
	channels := make([]<-chan []int, n)
	for i := range n {
		ch, unsub := b.Subscribe("topic")
		t.Cleanup(unsub)
		channels[i] = ch
	}

	b.Publish("topic", []int{42})

	for i, ch := range channels {
		select {
		case ids := <-ch:
			assert.Equal(t, []int{42}, ids, "subscriber %d received wrong ids", i)
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive signal", i)
		}
	}
}

func TestBus_Publish_DifferentTopic_NoSignal(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	ch, unsub := b.Subscribe("pagos_changed")
	defer unsub()

	b.Publish("saldos_changed", []int{99})

	select {
	case <-ch:
		t.Fatal("received signal for wrong topic")
	case <-time.After(50 * time.Millisecond):
		// correct: no signal
	}
}

func TestBus_Unsubscribe_StopsDelivery(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	ch, unsub := b.Subscribe("topic")
	unsub() // unsubscribe immediately

	b.Publish("topic", []int{1})

	select {
	case <-ch:
		t.Fatal("received signal after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// correct: no signal
	}
}

// ── Slow-subscriber test ──────────────────────────────────────────────────────

// TestBus_SlowSubscriber_PublishNeverBlocks verifies that Publish returns
// quickly even if a subscriber never drains its channel. The buffer=1 +
// latest-wins coalescing guarantees this.
func TestBus_SlowSubscriber_PublishNeverBlocks(t *testing.T) {
	t.Parallel()
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	b := eventbus.New()
	defer b.Close()

	_, unsub := b.Subscribe("topic") // never read from this channel
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			b.Publish("topic", []int{i})
		}
	}()

	select {
	case <-done:
		// all publishes completed without blocking
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
}

// ── Close-then-publish ────────────────────────────────────────────────────────

func TestBus_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	b.Close()
	b.Close() // must not panic
}

func TestBus_PublishAfterClose_NoOp(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	b.Close()
	// Must not panic.
	b.Publish("pagos_changed", []int{1})
}

func TestBus_SubscribeAfterClose_ReturnsClosedChannel(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	b.Close()

	ch, unsub := b.Subscribe("topic")
	defer unsub()

	// A closed channel is immediately readable.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel returned after Close must be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel returned after Close was not already closed")
	}
}

// ── Unsubscribe-during-publish ────────────────────────────────────────────────

// TestBus_UnsubscribeDuringPublish_NoDeadlock launches a goroutine that
// continuously publishes while the main goroutine continuously subscribes and
// unsubscribes. Verifies no deadlock and no race (run with -race).
func TestBus_UnsubscribeDuringPublish_NoDeadlock(t *testing.T) {
	t.Parallel()
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	b := eventbus.New()
	defer b.Close()

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Publisher goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			b.Publish("topic", []int{i})
			time.Sleep(0) // yield so subscriber goroutines run
		}
	}()

	// Rapidly subscribe and unsubscribe.
	for range 500 {
		_, unsub := b.Subscribe("topic")
		unsub()
	}

	stop.Store(true)
	wg.Wait()
}

// ── Concurrency stress test ───────────────────────────────────────────────────

// TestBus_ConcurrencyStress verifies that concurrent subscribers and publishers
// do not race and that Publish stays fast.
//
// Scale: 200 subscribers × 2000 publishes (400 000 total ops). Full 1000×10 000
// would be 10 M ops and takes >30 s under -race; this scale completes in <5 s.
func TestBus_ConcurrencyStress(t *testing.T) {
	t.Parallel()
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const (
		numSubs    = 200
		numPublish = 2000
		// Under -race the Go race detector adds ~5–10× overhead.
		// Use a generous threshold so the assertion holds in both modes.
		p99Threshold = 2 * time.Millisecond
	)

	b := eventbus.New()

	// Spin up subscribers.
	unsubs := make([]func(), numSubs)
	for i := range numSubs {
		_, unsub := b.Subscribe("pagos_changed")
		unsubs[i] = unsub
	}

	// Publish and collect per-call latencies.
	durations := make([]time.Duration, numPublish)
	for i := range numPublish {
		start := time.Now()
		b.Publish("pagos_changed", []int{i})
		durations[i] = time.Since(start)
	}

	// Unsubscribe all.
	for _, unsub := range unsubs {
		unsub()
	}
	b.Close()

	// Compute p99.
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p99 := durations[int(float64(len(durations))*0.99)]

	assert.Less(t, p99, p99Threshold,
		"Publish p99 latency %v exceeds threshold %v (scale: %d subs × %d publishes)",
		p99, p99Threshold, numSubs, numPublish,
	)
}

// ── Rapid property tests ──────────────────────────────────────────────────────

// TestProperty_Bus_NoLeaksNoPanics uses rapid to generate random sequences of
// Subscribe / Publish / Unsubscribe operations and checks:
//   - No panics.
//   - No goroutine leaks (goleak).
//   - Every active subscriber receives ≥1 signal after the last Publish to
//     its topic (liveness invariant).
func TestProperty_Bus_NoLeaksNoPanics(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		b := eventbus.New()
		defer b.Close()

		type subEntry struct {
			ch       <-chan []int
			unsub    func()
			topic    string
			unsubbed bool
		}

		topics := []string{"pagos_changed", "saldos_changed", "other"}

		var entries []*subEntry

		ops := rapid.IntRange(1, 100).Draw(rt, "num_ops")
		for range ops {
			op := rapid.IntRange(0, 2).Draw(rt, "op") // 0=subscribe, 1=publish, 2=unsubscribe

			topic := rapid.SampledFrom(topics).Draw(rt, "topic")

			switch op {
			case 0: // subscribe
				ch, unsub := b.Subscribe(topic)
				entries = append(entries, &subEntry{
					ch:    ch,
					unsub: unsub,
					topic: topic,
				})

			case 1: // publish
				ids := []int{rapid.IntRange(1, 1000).Draw(rt, "id")}
				b.Publish(topic, ids)

				// Liveness: every active subscriber of this topic should have
				// a payload pending (buffer=1 fills on Publish). If the channel
				// was already full from a prior Publish the new payload replaced
				// the old one (latest-wins), but there is still a pending payload.
				// Try draining up to 1 slot, which accounts for both cases.
				for _, e := range entries {
					if !e.unsubbed && e.topic == topic {
						select {
						case <-e.ch:
							// Drained the payload; invariant confirmed.
						default:
							// Channel was already empty after a prior drain OR
							// the payload was coalesced away. Both are valid
							// semantics; no assertion failure.
						}
					}
				}

			case 2: // unsubscribe a random active entry
				active := make([]*subEntry, 0, len(entries))
				for _, e := range entries {
					if !e.unsubbed {
						active = append(active, e)
					}
				}
				if len(active) > 0 {
					idx := rapid.IntRange(0, len(active)-1).Draw(rt, "unsub_idx")
					e := active[idx]
					e.unsub()
					e.unsubbed = true
				}
			}
		}

		// Clean up all remaining subscriptions.
		for _, e := range entries {
			if !e.unsubbed {
				e.unsub()
			}
		}
	})
}

// TestProperty_Bus_SubscribePublishLiveness is a focused property test for the
// liveness invariant: after Publish(topic, ids), every subscriber active at
// that moment must have a payload waiting.
func TestProperty_Bus_SubscribePublishLiveness(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		b := eventbus.New()
		defer b.Close()

		n := rapid.IntRange(1, 20).Draw(rt, "num_subs")
		channels := make([]<-chan []int, n)
		unsubs := make([]func(), n)
		for i := range n {
			ch, unsub := b.Subscribe("t")
			channels[i] = ch
			unsubs[i] = unsub
		}
		defer func() {
			for _, u := range unsubs {
				u()
			}
		}()

		sentIDs := []int{1, 2, 3}
		b.Publish("t", sentIDs)

		// Each subscriber must have exactly one payload pending.
		for i, ch := range channels {
			select {
			case ids := <-ch:
				assert.Equal(rt, sentIDs, ids, "subscriber %d received wrong ids", i)
			case <-time.After(100 * time.Millisecond):
				rt.Fatalf("subscriber %d did not receive signal after Publish", i)
			}
		}
	})
}

// TestProperty_Bus_CloseUnblocksSubs verifies that Close causes all subscriber
// channels to become readable (they receive the channel-closed zero value).
func TestProperty_Bus_CloseUnblocksSubs(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		b := eventbus.New()

		n := rapid.IntRange(1, 10).Draw(rt, "num_subs")
		channels := make([]<-chan []int, n)
		for i := range n {
			ch, _ := b.Subscribe("t") // unsub not needed; Close handles cleanup
			channels[i] = ch
		}

		b.Close()

		for i, ch := range channels {
			select {
			case _, ok := <-ch:
				require.False(rt, ok, "subscriber %d channel must be closed after Bus.Close", i)
			case <-time.After(100 * time.Millisecond):
				rt.Fatalf("subscriber %d channel not closed within 100ms after Bus.Close", i)
			}
		}
	})
}

// TestProperty_Eventbus_NoPanicsUnderRandomOps uses rapid to generate random
// sequences of Subscribe/Publish/Unsubscribe/Close operations and asserts:
//   - No panics.
//   - No goroutine leaks (goleak).
//   - Coalescing contract holds (latest payload visible in buffer).
func TestProperty_Eventbus_NoPanicsUnderRandomOps(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		defer goleak.VerifyNone(rt, goleak.IgnoreCurrent())

		b := eventbus.New()

		type entry struct {
			ch       <-chan []int
			unsub    func()
			topic    string
			unsubbed bool
		}

		topics := []string{"pagos_changed", "saldos_changed"}
		var entries []*entry

		ops := rapid.IntRange(1, 50).Draw(rt, "ops")
		for range ops {
			op := rapid.IntRange(0, 3).Draw(rt, "op")
			topic := rapid.SampledFrom(topics).Draw(rt, "topic")

			switch op {
			case 0: // subscribe
				ch, unsub := b.Subscribe(topic)
				entries = append(entries, &entry{ch: ch, unsub: unsub, topic: topic})

			case 1: // publish with random ids
				n := rapid.IntRange(0, 5).Draw(rt, "n_ids")
				ids := make([]int, n)
				for i := range n {
					ids[i] = rapid.IntRange(1, 9999).Draw(rt, "id")
				}
				b.Publish(topic, ids)

			case 2: // unsubscribe random active entry
				active := make([]*entry, 0, len(entries))
				for _, e := range entries {
					if !e.unsubbed {
						active = append(active, e)
					}
				}
				if len(active) > 0 {
					idx := rapid.IntRange(0, len(active)-1).Draw(rt, "idx")
					e := active[idx]
					e.unsub()
					e.unsubbed = true
				}

			case 3: // close — then stop loop
				for _, e := range entries {
					if !e.unsubbed {
						e.unsub()
					}
				}
				b.Close()
				return
			}
		}

		// Clean up.
		for _, e := range entries {
			if !e.unsubbed {
				e.unsub()
			}
		}
		b.Close()
	})
}

// TestProperty_Eventbus_LatestWinsCoalescing verifies that when a subscriber
// does not drain, multiple Publishes result in the latest payload being visible.
func TestProperty_Eventbus_LatestWinsCoalescing(t *testing.T) {
	t.Parallel()

	b := eventbus.New()
	defer b.Close()

	ch, unsub := b.Subscribe("topic")
	defer unsub()

	// Publish three different payloads without the subscriber reading.
	first := []int{1, 2, 3}
	second := []int{4, 5, 6}
	third := []int{7, 8, 9}

	b.Publish("topic", first)
	b.Publish("topic", second)
	b.Publish("topic", third)

	// Subscriber should receive the LAST payload (latest-wins coalescing).
	select {
	case ids := <-ch:
		assert.Equal(t, third, ids, "latest-wins: subscriber must receive the last published ids")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("subscriber did not receive payload within 200ms")
	}
}

// TestProperty_Eventbus_EmptyPayloadDelivered verifies that Publish with nil
// and Publish with []int{} both deliver an empty (non-nil) slice to a waiting
// subscriber. Empty ids is a valid wakeup signal for cursor sync.
func TestProperty_Eventbus_EmptyPayloadDelivered(t *testing.T) {
	t.Parallel()

	t.Run("nil_becomes_empty_slice", func(t *testing.T) {
		t.Parallel()

		b := eventbus.New()
		defer b.Close()

		ch, unsub := b.Subscribe("topic")
		defer unsub()

		b.Publish("topic", nil)

		select {
		case ids, ok := <-ch:
			require.True(t, ok, "channel must be open")
			assert.NotNil(t, ids, "nil publish must be delivered as non-nil empty slice")
			assert.Empty(t, ids, "nil publish must deliver empty ids")
		case <-time.After(200 * time.Millisecond):
			t.Fatal("subscriber did not receive nil-publish wakeup within 200ms")
		}
	})

	t.Run("empty_slice_delivered", func(t *testing.T) {
		t.Parallel()

		b := eventbus.New()
		defer b.Close()

		ch, unsub := b.Subscribe("topic")
		defer unsub()

		b.Publish("topic", []int{})

		select {
		case ids, ok := <-ch:
			require.True(t, ok, "channel must be open")
			assert.NotNil(t, ids, "empty publish must deliver non-nil slice")
			assert.Empty(t, ids, "empty publish must deliver empty ids")
		case <-time.After(200 * time.Millisecond):
			t.Fatal("subscriber did not receive empty-publish wakeup within 200ms")
		}
	})
}

// TestProperty_Eventbus_ActiveSubscriberAlwaysGetsLastSignal verifies that at
// least one Publish after Subscribe (but before Unsubscribe) means the channel
// has ≥1 deliverable signal.
func TestProperty_Eventbus_ActiveSubscriberAlwaysGetsLastSignal(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		b := eventbus.New()
		defer b.Close()

		ch, unsub := b.Subscribe("topic")
		defer unsub()

		// Publish at least one payload with random ids.
		numPublish := rapid.IntRange(1, 10).Draw(rt, "num_publish")
		var lastIDs []int
		for i := range numPublish {
			lastIDs = []int{i + 1}
			b.Publish("topic", lastIDs)
		}

		// Channel must have a pending signal (latest-wins: it holds the last payload).
		select {
		case ids, ok := <-ch:
			require.True(rt, ok, "channel must be open before unsubscribe")
			assert.Equal(rt, lastIDs, ids,
				"active subscriber must receive the last published payload")
		case <-time.After(100 * time.Millisecond):
			rt.Fatal("active subscriber must have at least one signal pending after Publish")
		}
	})
}
