package gandr

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"strings"
	"time"

	gandrclientdb "github.com/gandr-net/gandr/pkg/clientdb"
	gandrcrypto "github.com/gandr-net/gandr/pkg/crypto"
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
	groups map[[32]byte][32]byte
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
	return &Session{db: db, client: client, id: id, groups: make(map[[32]byte][32]byte)}, nil
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
	if s.client != nil {
		_ = s.client.Unsubscribe(ctx, id)
	}
	if err := s.db.LeaveChannel(id); err != nil {
		return nil, err
	}
	return s.Channels()
}

func (s *Session) SendChannel(ctx context.Context, id [32]byte, content string) error {
	if s == nil || s.id == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	payload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: id, Content: content})
	if err != nil {
		return err
	}
	env, err := proto.NewEnvelope(s.id.PrivateKey, proto.MsgChat, id, payload)
	if err != nil {
		return err
	}
	if s.client != nil {
		if err := s.client.Send(ctx, env); err != nil {
			return err
		}
	}
	return s.SaveMessage(ChatMessage{
		Hash: env.ContentID(), ChannelID: id, Sender: env.Sender,
		Content: content, At: env.Timestamp, Local: true,
	})
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

type ChatMessage struct {
	Hash      [32]byte
	ChannelID [32]byte
	Sender    [32]byte
	Content   string
	At        int64
	Local     bool
}

type PrivateGroup struct {
	ID        [32]byte
	Name      string
	CreatedAt int64
}

type PrivateGroupMessage struct {
	Hash    [32]byte
	GroupID [32]byte
	Sender  [32]byte
	Content string
	At      int64
}

type Peer struct {
	Identity [32]byte
	Address  string
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

// AddContact stores a local-only friend/petname. It is never serialized into
// a network message or public BACKFLASH cache object.
func (s *Session) AddContact(pubkey [32]byte, name, note string) error {
	if s == nil || s.db == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	return s.db.SetNickname(gandrclientdb.Nickname{Pubkey: pubkey, Name: name, Note: note})
}

func (s *Session) SaveMessage(m ChatMessage) error {
	if s == nil || s.db == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	return s.db.SaveChatMessage(gandrclientdb.ChatMessage{
		Hash: m.Hash, ChannelID: m.ChannelID, Sender: m.Sender,
		Content: m.Content, At: m.At, Local: m.Local,
	})
}

func (s *Session) Messages(channelID [32]byte, limit int) ([]ChatMessage, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	items, err := s.db.ListChatMessages(channelID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ChatMessage, 0, len(items))
	for _, item := range items {
		out = append(out, ChatMessage{
			Hash: item.Hash, ChannelID: item.ChannelID, Sender: item.Sender,
			Content: item.Content, At: item.At, Local: item.Local,
		})
	}
	return out, nil
}

func (s *Session) CreatePrivateGroup(name, password string) (PrivateGroup, error) {
	if s == nil || s.db == nil {
		return PrivateGroup{}, errors.New("GANDR-sessionen är inte aktiv")
	}
	if strings.TrimSpace(name) == "" {
		return PrivateGroup{}, errors.New("gruppnamn saknas")
	}
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return PrivateGroup{}, err
	}
	key, err := gandrcrypto.NewGroupKey()
	if err != nil {
		return PrivateGroup{}, err
	}
	salt, err := gandrcrypto.NewGroupSalt()
	if err != nil {
		return PrivateGroup{}, err
	}
	wrapped, err := gandrcrypto.WrapGroupKey([]byte(password), salt, id, key)
	if err != nil {
		return PrivateGroup{}, err
	}
	created := time.Now().UnixNano()
	if err := s.db.SavePrivateGroup(gandrclientdb.PrivateGroup{
		ID: id, Name: strings.TrimSpace(name), Salt: salt[:], WrappedKey: wrapped, CreatedAt: created,
	}); err != nil {
		return PrivateGroup{}, err
	}
	s.groups[id] = key
	return PrivateGroup{ID: id, Name: strings.TrimSpace(name), CreatedAt: created}, nil
}

func (s *Session) PrivateGroups() ([]PrivateGroup, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("GANDR-sessionen är inte aktiv")
	}
	items, err := s.db.ListPrivateGroups()
	if err != nil {
		return nil, err
	}
	out := make([]PrivateGroup, 0, len(items))
	for _, item := range items {
		out = append(out, PrivateGroup{ID: item.ID, Name: item.Name, CreatedAt: item.CreatedAt})
	}
	return out, nil
}

func (s *Session) UnlockPrivateGroup(id [32]byte, password string) error {
	if s == nil || s.db == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	items, err := s.db.ListPrivateGroups()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if len(item.Salt) != gandrcrypto.GroupSaltSize {
			return errors.New("ogiltig gruppsalt")
		}
		var salt [gandrcrypto.GroupSaltSize]byte
		copy(salt[:], item.Salt)
		key, err := gandrcrypto.UnwrapGroupKey([]byte(password), salt, id, item.WrappedKey)
		if err != nil {
			return errors.New("fel grupp-lösenord")
		}
		s.groups[id] = key
		return nil
	}
	return errors.New("gruppen finns inte lokalt")
}

// IsGroupUnlocked reports whether id's key is already cached in this
// session (from an earlier CreatePrivateGroup or UnlockPrivateGroup call in
// the same run), so the caller can skip re-prompting for the password.
func (s *Session) IsGroupUnlocked(id [32]byte) bool {
	if s == nil {
		return false
	}
	_, ok := s.groups[id]
	return ok
}

func (s *Session) SendPrivateGroup(id [32]byte, content string) (PrivateGroupMessage, error) {
	key, ok := s.groups[id]
	if !ok {
		return PrivateGroupMessage{}, errors.New("gruppen är låst")
	}
	blob, err := gandrcrypto.EncryptGroup(key, id, []byte(content))
	if err != nil {
		return PrivateGroupMessage{}, err
	}
	hash := gandrcrypto.Digest(id[:], blob)
	var sender [32]byte
	copy(sender[:], s.id.PublicKey)
	message := gandrclientdb.PrivateGroupMessage{Hash: hash, GroupID: id, Sender: sender, Ciphertext: blob, MessageAt: time.Now().UnixNano()}
	if err := s.db.SavePrivateGroupMessage(message); err != nil {
		return PrivateGroupMessage{}, err
	}
	return PrivateGroupMessage{Hash: hash, GroupID: id, Sender: sender, Content: content, At: message.MessageAt}, nil
}

func (s *Session) PrivateGroupMessages(id [32]byte, limit int) ([]PrivateGroupMessage, error) {
	key, ok := s.groups[id]
	if !ok {
		return nil, errors.New("gruppen är låst")
	}
	items, err := s.db.ListPrivateGroupMessages(id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]PrivateGroupMessage, 0, len(items))
	for _, item := range items {
		plain, err := gandrcrypto.DecryptGroup(key, id, item.Ciphertext)
		if err != nil {
			return nil, err
		}
		out = append(out, PrivateGroupMessage{Hash: item.Hash, GroupID: item.GroupID, Sender: item.Sender, Content: string(plain), At: item.MessageAt})
	}
	return out, nil
}

func (s *Session) Block(pubkey [32]byte, reason string) error {
	if s == nil || s.db == nil {
		return errors.New("GANDR-sessionen är inte aktiv")
	}
	return s.db.Block(pubkey, reason)
}

func (s *Session) Peers(ctx context.Context) ([]Peer, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	items, err := s.client.PeerList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Peer, 0, len(items))
	for _, item := range items {
		out = append(out, Peer{Identity: item.Identity, Address: item.Addr})
	}
	return out, nil
}

type Message struct {
	Hash      [32]byte
	ChannelID [32]byte
	Sender    [32]byte
	Content   string
	At        int64
	Local     bool
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
		Hash:      env.ContentID(),
		ChannelID: payload.ChannelID,
		Sender:    env.Sender,
		Content:   payload.Content,
		At:        env.Timestamp,
	}, nil
}
