package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// ReliableConn is the smallest boundary needed to adapt a message-oriented
// overlay connection. Gandr's network.Conn has this shape, but BACKFLASH does
// not import Gandr or pass its identity through this interface.
type ReliableConn interface {
	Send(context.Context, []byte) error
	Recv(context.Context) ([]byte, error)
	Close() error
}

// ReliableTransport adapts a reliable message connection to the public cache
// protocol. It is intentionally request/response serialized per connection.
type ReliableTransport struct {
	conn ReliableConn
	mu   sync.Mutex
}

func NewReliableTransport(conn ReliableConn) *ReliableTransport {
	return &ReliableTransport{conn: conn}
}

func (t *ReliableTransport) Request(request Message) (Message, error) {
	return t.RequestContext(context.Background(), request)
}

func (t *ReliableTransport) RequestContext(ctx context.Context, request Message) (Message, error) {
	if t == nil || t.conn == nil {
		return Message{}, errors.New("mesh-anslutning saknas")
	}
	b, err := json.Marshal(request)
	if err != nil {
		return Message{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.conn.Send(ctx, b); err != nil {
		return Message{}, err
	}
	response, err := t.conn.Recv(ctx)
	if err != nil {
		return Message{}, err
	}
	var out Message
	if err := json.Unmarshal(response, &out); err != nil {
		return Message{}, err
	}
	if len(out.Body) > maxMessageBody {
		return Message{}, errors.New("mesh-svaret är för stort")
	}
	return out, nil
}

func (t *ReliableTransport) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn.Close()
}
