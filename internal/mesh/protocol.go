package mesh

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

type Message struct {
	Type MessageType `json:"type"`
	Hash string      `json:"hash,omitempty"`
	Body []byte      `json:"body,omitempty"`
}
