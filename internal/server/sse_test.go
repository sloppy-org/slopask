package server

import (
	"testing"
	"time"
)

func TestBroker_SubscribeUnsubscribe(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub := b.subscribe(1)
	b.mu.RLock()
	count := len(b.subscribers)
	b.mu.RUnlock()
	if count != 1 {
		t.Errorf("subscriber count = %d, want 1", count)
	}

	b.unsubscribe(sub)
	b.mu.RLock()
	count = len(b.subscribers)
	b.mu.RUnlock()
	if count != 0 {
		t.Errorf("subscriber count = %d, want 0 after unsubscribe", count)
	}
}

func TestBroker_PublishReachesSubscribers(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub1 := b.subscribe(1)
	sub2 := b.subscribe(1)
	defer b.unsubscribe(sub1)
	defer b.unsubscribe(sub2)

	b.publish(1, "test_event", `{"msg":"hello"}`)

	select {
	case evt := <-sub1.ch:
		if evt.Name != "test_event" {
			t.Errorf("sub1 event name = %q, want %q", evt.Name, "test_event")
		}
		if evt.Data != `{"msg":"hello"}` {
			t.Errorf("sub1 event data = %q", evt.Data)
		}
	case <-time.After(time.Second):
		t.Error("sub1 timed out waiting for event")
	}

	select {
	case evt := <-sub2.ch:
		if evt.Name != "test_event" {
			t.Errorf("sub2 event name = %q", evt.Name)
		}
	case <-time.After(time.Second):
		t.Error("sub2 timed out waiting for event")
	}
}

func TestBroker_PublishIsolatesByRoom(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub1 := b.subscribe(1)
	sub2 := b.subscribe(2)
	defer b.unsubscribe(sub1)
	defer b.unsubscribe(sub2)

	b.publish(1, "room1_event", "data1")

	// sub1 (room 1) should receive.
	select {
	case evt := <-sub1.ch:
		if evt.Name != "room1_event" {
			t.Errorf("sub1 got %q", evt.Name)
		}
	case <-time.After(time.Second):
		t.Error("sub1 timed out")
	}

	// sub2 (room 2) should NOT receive.
	select {
	case evt := <-sub2.ch:
		t.Errorf("sub2 should not receive room 1 event, got %q", evt.Name)
	case <-time.After(50 * time.Millisecond):
		// Expected: no event for room 2.
	}
}

func TestBroker_SlowSubscriberDropped(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub := b.subscribe(1)
	defer b.unsubscribe(sub)

	// Fill the subscriber channel buffer (capacity is 64).
	for i := 0; i < 64; i++ {
		b.publish(1, "fill", "x")
	}

	// The next publish should be dropped (non-blocking).
	b.publish(1, "overflow", "dropped")

	// Drain and count: should have exactly 64 events, none with name "overflow".
	count := 0
	for {
		select {
		case evt := <-sub.ch:
			count++
			if evt.Name == "overflow" {
				t.Error("overflow event should have been dropped")
			}
		default:
			goto done
		}
	}
done:
	if count != 64 {
		t.Errorf("drained %d events, want 64", count)
	}
}

func TestBroker_MultipleRooms(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub1 := b.subscribe(10)
	sub2 := b.subscribe(20)
	sub3 := b.subscribe(10)
	defer b.unsubscribe(sub1)
	defer b.unsubscribe(sub2)
	defer b.unsubscribe(sub3)

	b.publish(10, "evt10", "data10")
	b.publish(20, "evt20", "data20")

	// sub1 and sub3 should get room 10 event.
	for _, sub := range []*subscriber{sub1, sub3} {
		select {
		case evt := <-sub.ch:
			if evt.Name != "evt10" {
				t.Errorf("expected evt10, got %q", evt.Name)
			}
		case <-time.After(time.Second):
			t.Error("timed out waiting for room 10 event")
		}
	}

	// sub2 should get room 20 event.
	select {
	case evt := <-sub2.ch:
		if evt.Name != "evt20" {
			t.Errorf("expected evt20, got %q", evt.Name)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for room 20 event")
	}

	// sub1 should NOT have room 20 events.
	select {
	case evt := <-sub1.ch:
		t.Errorf("sub1 should not receive room 20 event, got %q", evt.Name)
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestBroker_PublishAfterUnsubscribe(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub := b.subscribe(1)
	b.unsubscribe(sub)

	// Publish after unsubscribe should not panic or block.
	b.publish(1, "test", "data")

	select {
	case <-sub.ch:
		t.Error("unsubscribed subscriber should not receive events")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestBroker_ConcurrentPublish(t *testing.T) {
	t.Parallel()
	b := newSSEBroker()

	sub := b.subscribe(1)
	defer b.unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.publish(1, "concurrent", "data")
		}
		close(done)
	}()

	<-done

	count := 0
	for {
		select {
		case <-sub.ch:
			count++
		default:
			goto end
		}
	}
end:
	// At most 64 due to channel buffer; some may be dropped.
	if count == 0 {
		t.Error("expected at least some events")
	}
	if count > 64 {
		t.Errorf("received %d events, but channel buffer is 64", count)
	}
}
