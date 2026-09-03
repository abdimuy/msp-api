package eventbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBus_Unsubscribe_CleansUpEmptyTopicMap verifies that unsubscribing the
// last subscriber for a topic removes the topic key from the internal map.
// This is a white-box test; it uses internal package access to inspect the map
// so gremlins can kill the len(subs)==0 cleanup branch.
func TestBus_Unsubscribe_CleansUpEmptyTopicMap(t *testing.T) {
	t.Parallel()

	b := New()
	defer b.Close()

	_, unsub1 := b.Subscribe("topic")
	_, unsub2 := b.Subscribe("topic")

	b.mu.RLock()
	assert.Len(t, b.subs["topic"], 2, "two subscribers should exist before any unsubscribe")
	b.mu.RUnlock()

	unsub1()

	b.mu.RLock()
	assert.Len(t, b.subs["topic"], 1, "one subscriber should remain after first unsubscribe")
	topicPresent := b.subs["topic"] != nil
	b.mu.RUnlock()
	assert.True(t, topicPresent, "topic key should still exist with one subscriber")

	unsub2()

	b.mu.RLock()
	_, topicExists := b.subs["topic"]
	b.mu.RUnlock()
	assert.False(t, topicExists, "topic key must be removed after last subscriber unsubscribes")
}
