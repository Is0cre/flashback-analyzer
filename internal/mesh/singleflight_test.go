package mesh

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestObjectRetrievalIsSingleflightPerHash(t *testing.T) {
	store, err := OpenObjectStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	object := NewObject(ThreadPageSnapshot, "flashback", "t-single:1", time.Now(), []byte("single"), OriginVerified)
	var mu sync.Mutex
	requests := 0
	node := &Node{Store: store, Peer: HandlerTransport{Handler: func(Message) (Message, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		body, _ := marshalObjectForTest(object)
		return Message{Type: Object, Hash: object.HashString(), Body: body}, nil
	}}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := node.GetContext(context.Background(), object.HashString()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatalf("förväntade en peerförfrågan, fick %d", requests)
	}
}

func marshalObjectForTest(o CacheObject) ([]byte, error) {
	return json.Marshal(o)
}
