// Package eventbus provides a pure-Go, in-process signal-only fan-out bus.
// It is used to bridge the Firebird POST_EVENT listener with SSE handlers:
// the listener calls Publish and SSE handlers block on subscribed channels.
package eventbus

import (
	"sync"
	"sync/atomic"
)

type subscriber struct {
	id uint64
	ch chan struct{}
}

// Bus delivers signal-only fan-out for in-process topic notifications.
// Publish never blocks regardless of how many slow subscribers exist.
// All methods are safe for concurrent use.
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
// an empty struct whenever Publish is called for that topic, and an
// unsubscribe function that must be called when the caller no longer needs
// updates (typically deferred). The channel has buffer=1; signals delivered
// while the previous signal is unread are dropped (coalescing: the receiver
// only cares that something changed, not how many times).
//
// If the Bus is already closed, Subscribe returns an already-closed channel
// and a no-op unsubscribe so callers never deadlock.
func (b *Bus) Subscribe(topic string) (<-chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Return a pre-closed channel; caller will receive immediately and
		// can detect the close via the channel-drained boolean.
		ch := make(chan struct{})
		close(ch)
		return ch, func() {}
	}

	id := b.nextID.Add(1)
	sub := &subscriber{
		id: id,
		ch: make(chan struct{}, 1),
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

// Publish sends a signal to every subscriber of topic. Non-blocking: if a
// subscriber's channel already holds a pending signal the new one is dropped
// (the subscriber will still process the change). Publish acquires only a
// read lock so multiple goroutines may publish concurrently without
// contending with each other.
//
// If the Bus is closed, Publish is a no-op.
func (b *Bus) Publish(topic string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, s := range b.subs[topic] {
		select {
		case s.ch <- struct{}{}:
		default:
			// Subscriber already has a pending signal; drop the duplicate.
		}
	}
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
