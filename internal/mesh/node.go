package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/backflash-cli/backflash/internal/diagnostics"
)

// Node serves and retrieves public cache objects. A Transport implementation
// may later use Yggdrasil; it must not receive Gandr identity or Flashback
// cookies.
type Node struct {
	Store *ObjectStore
	Peer  Transport
	mu    sync.Mutex
	busy  map[string]*getCall
}

type getCall struct {
	done   chan struct{}
	object CacheObject
	err    error
}

func (n *Node) Serve(request Message) (Message, error) {
	if request.Type != Get {
		return Message{Type: NotFound}, fmt.Errorf("okänd meshförfrågan: %s", request.Type)
	}
	if n == nil || n.Store == nil {
		return Message{Type: NotFound}, errors.New("mesh-lagring saknas")
	}
	var o CacheObject
	var err error
	if request.Hash != "" {
		o, err = n.Store.Get(request.Hash)
	} else {
		o, err = n.Store.Find(request.Source, request.ResourceID, ObjectType(request.ObjectType))
	}
	if err != nil {
		return Message{Type: NotFound, Hash: request.Hash}, nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return Message{Type: NotFound}, err
	}
	return Message{Type: Object, Hash: o.HashString(), Body: b}, nil
}

func (n *Node) Get(hash string) (CacheObject, error) {
	return n.GetContext(context.Background(), hash)
}

func (n *Node) GetResource(ctx context.Context, source string, resourceID string, typ ObjectType) (CacheObject, error) {
	if n == nil || n.Store == nil {
		return CacheObject{}, errors.New("mesh-lagring saknas")
	}
	if o, err := n.Store.Find(source, resourceID, typ); err == nil {
		return o, nil
	}
	key := "resource:" + source + "\x00" + resourceID + "\x00" + string(typ)
	n.mu.Lock()
	if n.busy == nil {
		n.busy = make(map[string]*getCall)
	}
	if call := n.busy[key]; call != nil {
		n.mu.Unlock()
		select {
		case <-call.done:
			return call.object, call.err
		case <-ctx.Done():
			return CacheObject{}, ctx.Err()
		}
	}
	call := &getCall{done: make(chan struct{})}
	n.busy[key] = call
	n.mu.Unlock()
	object, err := n.fetchResource(ctx, source, resourceID, typ)
	n.mu.Lock()
	call.object, call.err = object, err
	delete(n.busy, key)
	close(call.done)
	n.mu.Unlock()
	return object, err
}

func (n *Node) fetchResource(ctx context.Context, source string, resourceID string, typ ObjectType) (CacheObject, error) {
	if n.Peer == nil {
		return CacheObject{}, errors.New("meshobjekt saknas lokalt och ingen peer är ansluten")
	}
	request := Message{Type: Get, Source: source, ResourceID: resourceID, ObjectType: string(typ)}
	var response Message
	var err error
	if transport, ok := n.Peer.(ContextTransport); ok {
		response, err = transport.RequestContext(ctx, request)
	} else {
		response, err = n.Peer.Request(request)
	}
	if err != nil {
		return CacheObject{}, err
	}
	return n.acceptPeerObject(response, "", source, resourceID, typ)
}

func (n *Node) GetContext(ctx context.Context, hash string) (CacheObject, error) {
	if n == nil || n.Store == nil {
		return CacheObject{}, errors.New("mesh-lagring saknas")
	}
	if o, err := n.Store.Get(hash); err == nil {
		return o, nil
	}
	n.mu.Lock()
	if n.busy == nil {
		n.busy = make(map[string]*getCall)
	}
	if call := n.busy[hash]; call != nil {
		n.mu.Unlock()
		select {
		case <-call.done:
			return call.object, call.err
		case <-ctx.Done():
			return CacheObject{}, ctx.Err()
		}
	}
	call := &getCall{done: make(chan struct{})}
	n.busy[hash] = call
	n.mu.Unlock()
	object, err := n.fetchContext(ctx, hash)
	n.mu.Lock()
	call.object, call.err = object, err
	delete(n.busy, hash)
	close(call.done)
	n.mu.Unlock()
	return object, err
}

func (n *Node) fetchContext(ctx context.Context, hash string) (CacheObject, error) {
	if n.Peer == nil {
		return CacheObject{}, errors.New("meshobjekt saknas lokalt och ingen peer är ansluten")
	}
	var response Message
	var err error
	request := Message{Type: Get, Hash: hash}
	if transport, ok := n.Peer.(ContextTransport); ok {
		response, err = transport.RequestContext(ctx, request)
	} else {
		response, err = n.Peer.Request(request)
	}
	if err != nil {
		return CacheObject{}, err
	}
	if response.Type != Object {
		return CacheObject{}, fmt.Errorf("mesh-peer hittade inte objektet: %s", response.Type)
	}
	return n.acceptPeerObject(response, hash, "", "", "")
}

func (n *Node) acceptPeerObject(response Message, expectedHash, source, resourceID string, typ ObjectType) (CacheObject, error) {
	verifyDone := diagnostics.Start("object_verify")
	defer verifyDone()
	var o CacheObject
	if err := json.Unmarshal(response.Body, &o); err != nil {
		return CacheObject{}, fmt.Errorf("meshobjekt är ogiltigt: %w", err)
	}
	if response.Hash != "" && response.Hash != o.HashString() {
		return CacheObject{}, errors.New("peer-svaret har fel objektadress")
	}
	if expectedHash != "" && o.HashString() != expectedHash {
		return CacheObject{}, errors.New("peer-svaret matchar inte begärd hash")
	}
	if source != "" && (o.Source != source || o.ResourceID != resourceID || o.Type != typ) {
		return CacheObject{}, errors.New("peerobjektets resurs matchar inte begäran")
	}
	o.Provenance = PeerOnly
	persistDone := diagnostics.Start("object_persist")
	defer persistDone()
	if err := n.Store.Put(o); err != nil {
		return CacheObject{}, err
	}
	return o, nil
}

// HandlerTransport is a small adapter used by tests and local integrations.
// A Yggdrasil transport can implement the same Transport interface later.
type HandlerTransport struct {
	Handler func(Message) (Message, error)
}

func (t HandlerTransport) Request(m Message) (Message, error) {
	if t.Handler == nil {
		return Message{}, errors.New("mesh-transport saknar handler")
	}
	return t.Handler(m)
}

func (HandlerTransport) Close() error { return nil }
