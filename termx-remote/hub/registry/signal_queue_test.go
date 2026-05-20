package registry

import "testing"

func TestSignalQueueUsesProvidedKeyFunction(t *testing.T) {
	type testItem struct {
		Key string
	}
	queue := newSignalQueue(func(item testItem) string {
		return item.Key
	})

	queue.enqueue("mach_1", testItem{Key: "item_1"})
	if _, ok := queue.get("item_1"); !ok {
		t.Fatal("signalQueue did not store item under keyFn key")
	}
	item, ok := queue.dequeue("mach_1")
	if !ok || item.Key != "item_1" {
		t.Fatalf("dequeue = %+v, %v", item, ok)
	}
}

func TestSignalQueueRequiresKeyFunction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("newSignalQueue did not panic without keyFn")
		}
	}()
	_ = newSignalQueue[testSignalQueueItem](nil)
}

type testSignalQueueItem struct {
	Key string
}
