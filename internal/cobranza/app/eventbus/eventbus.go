// Package eventbus provides a pure-Go, in-process fan-out bus that carries
// an IDs payload. It is used to bridge the Firebird POST_EVENT listener with
// SSE handlers: the listener calls Publish with the IDs that changed and SSE
// handlers block on subscribed channels to emit those IDs to clients.
package eventbus

import (
	"sync"
	"sync/atomic"
)

type subscriber struct {
	id uint64
	ch chan []int
}

// Bus delivers fan-out IDs payloads for in-process topic notifications.
// Publish never blocks regardless of how many slow subscribers exist.
// All methods are safe for concurrent use.
//
// Buffer=1 with latest-wins coalescing: if Publish finds a subscriber's
// channel buffer occupied, it drains the old payload and writes the new one.
// The subscriber will process whatever is latest; intermediate IDs are lost
// but the subscriber will catch up via cursor sync on the next tick anyway.
//
// Empty []int{} is a VALID payload — it is explicitly delivered to wake
// subscribers for a cursor sync round. nil is treated identically to []int{}.
//
// After Close, subscriber channels are closed. Receiver code using
// "for ids := range ch" will see the channel close and exit. The final
// zero-value (nil slice) from a closed channel is harmless — callers should
// check the ok boolean from a two-value receive.
type Bus struct {
	mu     sync.RWMutex
	subs   map[string]map[uint64]*subscriber // topic -> id -> sub
	nextID atomic.Uint64
	closed bool
}

// New returns a ready-to-use Bus.
func New() *Bus {
	return &Bus{
		subs: make(map[string]map[uint64]*subscriber),
	}
}

// Subscribe registers interest in topic. It returns a channel that receives
// a []int payload whenever Publish is called for that topic, and an
// unsubscribe function that must be called when the caller no longer needs
// updates (typically deferred). The channel has buffer=1; if a new Publish
// arrives while the previous payload is unread, the newer payload replaces
// the older one (latest-wins coalescing).
//
// If the Bus is already closed, Subscribe returns an already-closed channel
// and a no-op unsubscribe so callers never deadlock.
func (b *Bus) Subscribe(topic string) (<-chan []int, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Return a pre-closed channel; caller will receive immediately and
		// can detect the close via the channel-drained boolean.
		ch := make(chan []int)
		close(ch)
		return ch, func() {}
	}

	id := b.nextID.Add(1)
	sub := &subscriber{
		id: id,
		ch: make(chan []int, 1),
	}

	if b.subs[topic] == nil {
		b.subs[topic] = make(map[uint64]*subscriber)
	}
	b.subs[topic][id] = sub

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if subs, ok := b.subs[topic]; ok {
			delete(subs, id)
			if len(subs) == 0 {
				delete(b.subs, topic)
			}
		}
	}

	return sub.ch, unsubscribe
}

// Publish sends ids to every subscriber of topic using latest-wins coalescing.
// Non-blocking: if a subscriber's channel already holds a pending payload the
// old payload is drained and replaced with the new one (latest wins). Publish
// acquires only a read lock so multiple goroutines may publish concurrently
// without contending with each other.
//
// nil ids is treated identically to []int{} — it is still delivered as a
// wakeup signal. Empty ids is a valid payload meaning "something changed,
// do a cursor sync".
//
// If the Bus is closed, Publish is a no-op.
func (b *Bus) Publish(topic string, ids []int) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}
	if ids == nil {
		ids = []int{}
	}

	for _, s := range b.subs[topic] {
		// Latest-wins: drain stale payload, then push new one.
		select {
		case s.ch <- ids:
			// Empty slot — direct deliver.
		default:
			// Slot occupied; drain the old payload and deliver the newer one.
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- ids:
			default:
				// Should not happen: we hold RLock so no concurrent writer races.
				// If it does (e.g. channel already closed), skip — subscriber
				// will catch up via cursor sync.
			}
		}
	}
}

// SubscriberCount returns the number of active subscribers for topic.
// It acquires a read lock so it is safe for concurrent use and does not
// contend with Publish.
func (b *Bus) SubscriberCount(topic string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[topic])
}

// Close shuts down the Bus. It closes every subscriber channel (so range
// loops and blocking reads on subscriber channels will unblock) and clears
// the internal map. Idempotent: multiple Close calls are safe.
//
// After Close: Publish is a no-op; Subscribe returns a pre-closed channel.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for topic, subs := range b.subs {
		for id, s := range subs {
			close(s.ch)
			delete(subs, id)
		}
		delete(b.subs, topic)
	}
}
