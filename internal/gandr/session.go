package gandr

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"

	gandrclientdb "github.com/gandr-net/gandr/pkg/clientdb"
	gandridentity "github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/proto"
)

// Session is the deliberately short-lived BACKFLASH-side handle to GANDR's
// private client layer. It is created only after the user unlocks the GANDR
// identity. BACKFLASH never puts this database, socket or message stream into
// the public cache mesh.
type Session struct {
	db     *gandrclientdb.DB
	client *ipc.Client
	id     *gandridentity.Identity
}

// Connect opens GANDR's encrypted client database. Network transport is
// optional: BACKFLASH can run the private GANDR client locally while the
// in-process transport is unavailable.
func (s *Subsystem) Connect(socketPath string) (*Session, error) {
	if s == nil {
		return nil, errors.New("GANDR-gränsen saknas")
	}
	s.mu.RLock()
	id := s.identity
	keyPath := s.path
	s.mu.RUnlock()
	if id == nil {
		return nil, errors.New("GANDR-valvet är låst")
	}
	dataDir := filepath.Dir(keyPath)
	db, err := gandrclientdb.Open(filepath.Join(dataDir, "client.db"), id.PrivateKey)
	if err != nil {
		return nil, err
	}
	var client *ipc.Client
	if socketPath != "" {
		client, err = ipc.Dial(socketPath)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &Session{db: db, client: client, id: id}, nil
}

// Close releases the private client database and IPC connection.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Channels returns locally joined channels without exposing the encrypted DB
// to the rest of BACKFLASH.
func (s *Session) Channels() ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	channels, err := s.db.ListChannels()
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, Channel{ID: ch.ID, Name: ch.Name})
	}
	return out, nil
}

// Incoming is the daemon's private message stream. Callers must only consume
// it while the GANDR view/session is active.
func (s *Session) Incoming() <-chan *proto.Envelope {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Incoming()
}

// Done reports a daemon disconnect.
func (s *Session) Done() <-chan struct{} {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Done()
}

// Subscribe joins a GANDR channel through the local daemon.
func (s *Session) Subscribe(ctx context.Context, id [32]byte) error {
	if s == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	if s.client == nil {
		return nil
	}
	return s.client.Subscribe(ctx, id)
}

func (s *Session) Join(ctx context.Context, name string) ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	id := ChannelID(name)
	if err := s.db.JoinChannel(id, name); err != nil {
		return nil, err
	}
	if err := s.Subscribe(ctx, id); err != nil {
		return nil, err
	}
	return s.Channels()
}

func (s *Session) Leave(ctx context.Context, id [32]byte) ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	_ = s.client.Unsubscribe(ctx, id)
	if err := s.db.LeaveChannel(id); err != nil {
		return nil, err
	}
	return s.Channels()
}

func (s *Session) SendChannel(ctx context.Context, id [32]byte, content string) error {
	if s == nil || s.id == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	if s.client == nil {
		return nil
	}
	payload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: id, Content: content})
	if err != nil {
		return err
	}
	env, err := proto.NewEnvelope(s.id.PrivateKey, proto.MsgChat, id, payload)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, env)
}

func (s *Session) Online() bool { return s != nil && s.client != nil }

func ChannelID(name string) [32]byte {
	return sha256.Sum256([]byte("gandr-channel:" + name))
}

type Channel struct {
	ID   [32]byte
	Name string
}

type Contact struct {
	Pubkey    [32]byte
	Name      string
	Note      string
	TrustHint uint8
}

var DefaultChannels = []string{"general", "support", "backflash", "offtopic"}

func (s *Session) EnsureDefaultChannels(ctx context.Context) ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	channels, err := s.Channels()
	if err != nil {
		return nil, err
	}
	if len(channels) > 0 {
		return channels, nil
	}
	for _, name := range DefaultChannels {
		if _, err := s.Join(ctx, name); err != nil {
			return nil, err
		}
	}
	return s.Channels()
}

func (s *Session) Contacts() ([]Contact, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	items, err := s.db.ListNicknames()
	if err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(items))
	for _, item := range items {
		out = append(out, Contact{Pubkey: item.Pubkey, Name: item.Name, Note: item.Note, TrustHint: item.TrustHint})
	}
	return out, nil
}

func (s *Session) Block(pubkey [32]byte, reason string) error {
	if s == nil || s.db == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	return s.db.Block(pubkey, reason)
}

type Message struct {
	ChannelID [32]byte
	Sender    [32]byte
	Content   string
	At        int64
}

func DecodeChat(env *proto.Envelope) (Message, error) {
	if env == nil || env.Type != proto.MsgChat {
		return Message{}, errors.New("GANDR-paketet är inte ett chattmeddelande")
	}
	payload := &proto.ChatPayload{}
	if err := proto.DecodePayload(env.Payload, payload); err != nil {
		return Message{}, err
	}
	return Message{
		ChannelID: payload.ChannelID,
		Sender:    env.Sender,
		Content:   payload.Content,
		At:        env.Timestamp,
	}, nil
}
