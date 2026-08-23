package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Node serves and retrieves public cache objects. A Transport implementation
// may later use Yggdrasil; it must not receive Gandr identity or Flashback
// cookies.
type Node struct {
	Store *ObjectStore
	Peer  Transport
}

func (n *Node) Serve(request Message) (Message, error) {
	if request.Type != Get {
		return Message{Type: NotFound}, fmt.Errorf("okänd meshförfrågan: %s", request.Type)
	}
	if n == nil || n.Store == nil {
		return Message{Type: NotFound}, errors.New("mesh-lagring saknas")
	}
	o, err := n.Store.Get(request.Hash)
	if err != nil {
		return Message{Type: NotFound, Hash: request.Hash}, nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return Message{Type: NotFound}, err
	}
	return Message{Type: Object, Hash: request.Hash, Body: b}, nil
}

func (n *Node) Get(hash string) (CacheObject, error) {
	return n.GetContext(context.Background(), hash)
}

func (n *Node) GetContext(ctx context.Context, hash string) (CacheObject, error) {
	if n == nil || n.Store == nil {
		return CacheObject{}, errors.New("mesh-lagring saknas")
	}
	if o, err := n.Store.Get(hash); err == nil {
		return o, nil
	}
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
	var o CacheObject
	if err := json.Unmarshal(response.Body, &o); err != nil {
		return CacheObject{}, fmt.Errorf("meshobjekt är ogiltigt: %w", err)
	}
	if response.Hash != "" && response.Hash != o.HashString() {
		return CacheObject{}, errors.New("peer-svaret har fel objektadress")
	}
	if o.HashString() != hash {
		return CacheObject{}, errors.New("peer-svaret matchar inte begärd hash")
	}
	o.Provenance = PeerOnly
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
