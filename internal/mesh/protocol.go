package mesh

import "context"

type MessageType string

const (
	Hello    MessageType = "HELLO"
	Have     MessageType = "HAVE"
	Get      MessageType = "GET"
	Object   MessageType = "OBJECT"
	NotFound MessageType = "NOT_FOUND"
	Manifest MessageType = "MANIFEST"
)

// Request/response transport is intentionally abstract. A future Yggdrasil
// adapter implements this boundary; the cache protocol never receives a
// Gandr identity or a Flashback cookie.
type Transport interface {
	Request(Message) (Message, error)
	Close() error
}

type ContextTransport interface {
	Transport
	RequestContext(context.Context, Message) (Message, error)
}

type Message struct {
	Type       MessageType `json:"type"`
	ID         string      `json:"id,omitempty"`
	Hash       string      `json:"hash,omitempty"`
	Source     string      `json:"source,omitempty"`
	ResourceID string      `json:"resource_id,omitempty"`
	ObjectType string      `json:"object_type,omitempty"`
	Body       []byte      `json:"body,omitempty"`
}
