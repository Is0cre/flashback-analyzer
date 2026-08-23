package mesh

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
)

const maxMessageBody = MaxObjectSize + 1<<20

// ConnTransport frames mesh messages as newline-delimited JSON over a
// connection. A Yggdrasil adapter only needs to provide a net.Conn; the
// protocol remains independent of routing, identity and Gandr.
type ConnTransport struct {
	conn net.Conn
	mu   sync.Mutex
	dec  *json.Decoder
	enc  *json.Encoder
}

func NewConnTransport(conn net.Conn) *ConnTransport {
	return &ConnTransport{conn: conn, dec: json.NewDecoder(bufio.NewReader(conn)), enc: json.NewEncoder(conn)}
}

func (t *ConnTransport) Request(request Message) (Message, error) {
	if t == nil || t.conn == nil {
		return Message{}, errors.New("mesh-anslutning saknas")
	}
	if len(request.Body) > maxMessageBody {
		return Message{}, errors.New("meshförfrågan är för stor")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.enc.Encode(request); err != nil {
		return Message{}, err
	}
	var response Message
	if err := t.dec.Decode(&response); err != nil {
		return Message{}, err
	}
	if len(response.Body) > maxMessageBody {
		return Message{}, errors.New("mesh-svaret är för stort")
	}
	return response, nil
}

func (t *ConnTransport) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

// ServeConn handles requests until the connection closes. It is suitable for
// a future Yggdrasil accept loop and never persists peer identity metadata.
func ServeConn(conn net.Conn, node *Node) error {
	if conn == nil {
		return errors.New("mesh-anslutning saknas")
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	enc := json.NewEncoder(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(line) > maxMessageBody+4096 {
			return errors.New("meshförfrågan är för stor")
		}
		var request Message
		if err := json.Unmarshal(line, &request); err != nil {
			return err
		}
		if len(request.Body) > maxMessageBody {
			return errors.New("meshförfrågan är för stor")
		}
		response, err := node.Serve(request)
		if err != nil {
			response = Message{Type: NotFound, Hash: request.Hash}
		}
		if len(response.Body) > maxMessageBody {
			return errors.New("mesh-svaret är för stort")
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
}
