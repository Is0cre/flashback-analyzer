package mesh

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeReliableConn struct{ response []byte }

func (f *fakeReliableConn) Send(_ context.Context, request []byte) error {
	var message Message
	if err := json.Unmarshal(request, &message); err != nil {
		return err
	}
	f.response, _ = json.Marshal(Message{Type: Object, Hash: message.Hash})
	return nil
}
func (f *fakeReliableConn) Recv(context.Context) ([]byte, error) { return f.response, nil }
func (f *fakeReliableConn) Close() error                         { return nil }

func TestReliableTransportAdaptsMessageConnection(t *testing.T) {
	transport := NewReliableTransport(&fakeReliableConn{})
	response, err := transport.Request(Message{Type: Get, Hash: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Type != Object || response.Hash != "abc" {
		t.Fatalf("oväntat svar: %+v", response)
	}
}
