package gandr

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	gandrclientdb "github.com/gandr-net/gandr/pkg/clientdb"
	gandrcrypto "github.com/gandr-net/gandr/pkg/crypto"
	gandridentity "github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/proto"
)

// daemonClient is the subset of *ipc.Client's behavior Session needs.
// embeddedClient (embedded.go) satisfies this too, wrapping an in-process
// *daemon.Daemon instead of a Unix socket — Session's methods never need
// to know or care which one they're talking to.
type daemonClient interface {
	Close() error
	Incoming() <-chan *proto.Envelope
	Done() <-chan struct{}
	Subscribe(ctx context.Context, channel [32]byte) error
	Unsubscribe(ctx context.Context, channel [32]byte) error
	Send(ctx context.Context, env *proto.Envelope) error
	Connect(ctx context.Context, yggKey [32]byte) error
	PeerList(ctx context.Context) ([]ipc.PeerInfo, error)
}

// Session is the deliberately short-lived BACKFLASH-side handle to GANDR's
// private client layer. It is created only after the user unlocks the GANDR
// identity. BACKFLASH never puts this database, socket or message stream into
// the public cache mesh.
type Session struct {
	db     *gandrclientdb.DB
	client daemonClient
	id     *gandridentity.Identity
	groups map[[32]byte][32]byte
}

// Connect opens GANDR's encrypted client database and, if socketPath is
// set, dials an external gandrd over its Unix socket. This is the
// power-user/self-hosted path; ordinary users go through
// Subsystem.ConnectEmbedded (embedded.go) instead, which needs no
// separate daemon process at all. socketPath == "" gives a fully
// offline, local-only session — used directly by tests, and as
// connectGandr's last-resort fallback in internal/tui/app.go.
func (s *Subsystem) Connect(socketPath string) (*Session, error) {
	if s == nil {
		return nil, errors.New("E2E-CHATT-gränsen saknas")
	}
	s.mu.RLock()
	id := s.identity
	keyPath := s.path
	s.mu.RUnlock()
	if id == nil {
		return nil, errors.New("E2E-CHATT-valvet är låst")
	}
	dataDir := filepath.Dir(keyPath)
	db, err := gandrclientdb.Open(filepath.Join(dataDir, "client.db"), id.PrivateKey)
	if err != nil {
		return nil, err
	}
	// Built as an interface value from the start, and only ever assigned
	// a genuinely non-nil concrete client — assigning a nil *ipc.Client
	// into an already-interface-typed variable would produce a non-nil
	// interface wrapping a nil pointer, silently breaking every
	// `s.client != nil` check below.
	var client daemonClient
	if socketPath != "" {
		dialed, err := ipc.Dial(socketPath)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		client = dialed
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
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	if s.client == nil {
		return nil
	}
	return s.client.Subscribe(ctx, id)
}

func (s *Session) Join(ctx context.Context, name string) ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
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

// EnsureDefaultChannels makes sure the default channels exist in local
// storage (joining any that don't yet) and — critically, on every call,
// not just the first — (re)subscribes this session's transport to all of
// them. Subscription state lives on the transport/daemon connection, not
// in local storage: a brand new embedded daemon (or a brand new IPC
// connection to an external one) always starts with zero subscriptions
// regardless of what channels were already known locally from a previous
// run. An earlier version of this function returned early once channels
// existed locally, which meant every app restart after the very first
// one silently left the fresh session subscribed to nothing — sends
// still worked, nothing was ever received. That's the actual bug behind
// "messages don't route": it wasn't the network, it was never asking to
// be delivered anything in the first place.
func (s *Session) EnsureDefaultChannels(ctx context.Context) ([]Channel, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	channels, err := s.Channels()
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		for _, name := range DefaultChannels {
			if _, err := s.Join(ctx, name); err != nil {
				return nil, err
			}
		}
		return s.Channels()
	}
	for _, channel := range channels {
		if err := s.Subscribe(ctx, channel.ID); err != nil {
			return nil, err
		}
	}
	return channels, nil
}

func (s *Session) Contacts() ([]Contact, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	return s.db.SetNickname(gandrclientdb.Nickname{Pubkey: pubkey, Name: name, Note: note})
}

func (s *Session) SaveMessage(m ChatMessage) error {
	if s == nil || s.db == nil {
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	return s.db.SaveChatMessage(gandrclientdb.ChatMessage{
		Hash: m.Hash, ChannelID: m.ChannelID, Sender: m.Sender,
		Content: m.Content, At: m.At, Local: m.Local,
	})
}

func (s *Session) Messages(channelID [32]byte, limit int) ([]ChatMessage, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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

// PruneOldMessages bounds local channel-history retention to maxAge. A
// client only ever has history for whatever it happened to be online
// for — there's no network-side backfill — so this is the only thing
// actually controlling how much accumulates locally over time. Private
// group messages are untouched: lower volume, more deliberately kept.
func (s *Session) PruneOldMessages(maxAge time.Duration) error {
	if s == nil || s.db == nil {
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	_, err := s.db.PruneChatMessages(maxAge, time.Now())
	return err
}

func (s *Session) CreatePrivateGroup(name, password string) (PrivateGroup, error) {
	if s == nil || s.db == nil {
		return PrivateGroup{}, errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	if strings.TrimSpace(name) == "" {
		return PrivateGroup{}, errors.New("gruppnamn saknas")
	}
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return PrivateGroup{}, err
	}
	// The key is derived from (id, password), not stored — see
	// gandrcrypto.DeriveGroupKey. Nothing here is secret to protect at
	// rest beyond the password itself, which is never persisted.
	key, err := gandrcrypto.DeriveGroupKey([]byte(password), id)
	if err != nil {
		return PrivateGroup{}, err
	}
	created := time.Now().UnixNano()
	if err := s.db.SavePrivateGroup(gandrclientdb.PrivateGroup{
		ID: id, Name: strings.TrimSpace(name), CreatedAt: created,
	}); err != nil {
		return PrivateGroup{}, err
	}
	s.groups[id] = key
	s.subscribeGroup(id)
	return PrivateGroup{ID: id, Name: strings.TrimSpace(name), CreatedAt: created}, nil
}

// subscribeGroup tells the transport this session wants broadcasts for
// id. Private groups ride the exact same proto.MsgChat channel-broadcast
// mechanism a public channel uses (see SendPrivateGroup), and the
// transport's own delivery filter (embeddedClient.wants /
// serverConn.wants) only lets through channel ids the session has
// explicitly subscribed to — without this, group messages would arrive
// over the wire and then get silently dropped before DecryptGroupMessage
// ever saw them. Best-effort: an offline session has no client to
// subscribe on, and that's fine, everything still works locally.
func (s *Session) subscribeGroup(id [32]byte) {
	if s.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.client.Subscribe(ctx, id)
}

// dmGroupID derives a stable channel id for a 1:1 conversation between
// two identities. Sorting the pair before hashing means both sides land
// on the same id regardless of who starts the conversation.
func dmGroupID(a, b [32]byte) [32]byte {
	first, second := a, b
	if bytes.Compare(first[:], second[:]) > 0 {
		first, second = second, first
	}
	h := sha256.New()
	h.Write([]byte("gandr-dm-id-v1:"))
	h.Write(first[:])
	h.Write(second[:])
	var id [32]byte
	copy(id[:], h.Sum(nil))
	return id
}

// StartDirectMessage opens (creating locally on first use) a private 1:1
// conversation with peerPubkey, stored and sent exactly like a private
// group of two. Unlike CreatePrivateGroup no password ever changes
// hands: the message key is a genuine X25519 ECDH shared secret between
// the two identities, so both sides derive the same key from nothing but
// already-public information, and — unlike a password-derived group key
// — nobody who merely observes both public keys (including any relay)
// can derive it too.
func (s *Session) StartDirectMessage(peerPubkey [32]byte, peerName string) (PrivateGroup, error) {
	if s == nil || s.db == nil || s.id == nil {
		return PrivateGroup{}, errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	id := dmGroupID(s.PublicKey(), peerPubkey)
	if _, unlocked := s.groups[id]; !unlocked {
		myX, err := gandrcrypto.PrivateKeyToX25519(s.id.PrivateKey)
		if err != nil {
			return PrivateGroup{}, err
		}
		theirX, err := gandrcrypto.PublicKeyToX25519(ed25519.PublicKey(peerPubkey[:]))
		if err != nil {
			return PrivateGroup{}, err
		}
		key, err := gandrcrypto.DeriveSharedKey(myX, theirX, "gandr-dm-v1")
		if err != nil {
			return PrivateGroup{}, err
		}
		s.groups[id] = key
	}
	name := strings.TrimSpace(peerName)
	if name == "" {
		name = fmt.Sprintf("~%x", peerPubkey[:4])
	}
	existing, err := s.db.ListPrivateGroups()
	if err != nil {
		return PrivateGroup{}, err
	}
	for _, g := range existing {
		if g.ID == id {
			s.subscribeGroup(id)
			return PrivateGroup{ID: g.ID, Name: g.Name, CreatedAt: g.CreatedAt}, nil
		}
	}
	created := time.Now().UnixNano()
	if err := s.db.SavePrivateGroup(gandrclientdb.PrivateGroup{ID: id, Name: name, CreatedAt: created}); err != nil {
		return PrivateGroup{}, err
	}
	s.subscribeGroup(id)
	return PrivateGroup{ID: id, Name: name, CreatedAt: created}, nil
}

func (s *Session) PrivateGroups() ([]PrivateGroup, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("E2E-CHATT-sessionen är inte aktiv")
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

// UnlockPrivateGroup makes id's group readable/writable in this session.
// The key is deterministically re-derived from (id, password) every
// time (see gandrcrypto.DeriveGroupKey) rather than looked up from local
// state, so this works identically for a group you created yourself and
// one you're joining for the first time via someone else's invite — see
// internal/gandr.EncodeGroupInvite, which carries exactly (id, name,
// password) and nothing else.
//
// A wrong password isn't rejected here — deriving a key never fails,
// there's nothing stored locally to fail to decrypt against. It surfaces
// instead the first time existing group history is decrypted (see
// PrivateGroupMessages) or a message fails to send meaningfully; a
// brand-new empty group has no way to verify a password at all. name is
// only used the first time this id is seen locally, to bookmark it for
// /grupp lista — an already-known group keeps whatever name it was
// first saved under.
func (s *Session) UnlockPrivateGroup(id [32]byte, password, name string) error {
	if s == nil || s.db == nil {
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	key, err := gandrcrypto.DeriveGroupKey([]byte(password), id)
	if err != nil {
		return err
	}
	s.groups[id] = key
	s.subscribeGroup(id)
	items, err := s.db.ListPrivateGroups()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == id {
			return nil // already bookmarked locally; key is now cached
		}
	}
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("~%x", id[:4])
	}
	return s.db.SavePrivateGroup(gandrclientdb.PrivateGroup{
		ID: id, Name: strings.TrimSpace(name), CreatedAt: time.Now().UnixNano(),
	})
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

// VerifyGroupPassword reports whether password re-derives the same key
// already cached for id, without touching that cache either way. Use
// this instead of re-running UnlockPrivateGroup on an already-open
// group (e.g. re-confirming a password before generating an invite) —
// UnlockPrivateGroup unconditionally overwrites the cached key with
// whatever it derives, so a typo there would silently swap in a wrong
// key for a group that was already correctly unlocked.
func (s *Session) VerifyGroupPassword(id [32]byte, password string) bool {
	if s == nil {
		return false
	}
	cached, ok := s.groups[id]
	if !ok {
		return false
	}
	got, err := gandrcrypto.DeriveGroupKey([]byte(password), id)
	return err == nil && got == cached
}

// SendPrivateGroup encrypts content with the group's shared symmetric key
// and broadcasts it under ChannelID = the group's own random id, the same
// proto.MsgChat mechanism a public channel uses — the only difference is
// the Content field holds base64-encoded ciphertext instead of plain
// text. Nothing about the wire format changes: a relaying node still
// only ever sees a channel broadcast and the sender's pubkey (unavoidable
// for signature verification, true of every message type), never
// readable content — the group id itself is random, not derived from a
// guessable name the way public channels' ids are.
//
// Previously this only ever wrote to the local encrypted DB and never
// actually reached the network — a private group looked like shared
// chat but was really a local notebook. ctx bounds the network attempt;
// like SendChannel, a failed send returns the error before anything is
// saved locally, and an offline session (no client) skips straight to
// local save.
func (s *Session) SendPrivateGroup(ctx context.Context, id [32]byte, content string) (PrivateGroupMessage, error) {
	if s == nil || s.id == nil {
		return PrivateGroupMessage{}, errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	key, ok := s.groups[id]
	if !ok {
		return PrivateGroupMessage{}, errors.New("gruppen är låst")
	}
	blob, err := gandrcrypto.EncryptGroup(key, id, []byte(content))
	if err != nil {
		return PrivateGroupMessage{}, err
	}
	if s.client != nil {
		payload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: id, Content: base64.StdEncoding.EncodeToString(blob)})
		if err != nil {
			return PrivateGroupMessage{}, err
		}
		env, err := proto.NewEnvelope(s.id.PrivateKey, proto.MsgChat, id, payload)
		if err != nil {
			return PrivateGroupMessage{}, err
		}
		if err := s.client.Send(ctx, env); err != nil {
			return PrivateGroupMessage{}, err
		}
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

// DecryptGroupMessage attempts to decode env as a message in one of this
// session's currently unlocked private groups. ok is false whenever env
// isn't a group message this session can read — including all ordinary
// public-channel traffic, and group messages for groups that are locked
// or unknown here — in which case the caller should fall through to
// normal public-channel decoding (see DecodeChat).
func (s *Session) DecryptGroupMessage(env *proto.Envelope) (msg PrivateGroupMessage, ok bool) {
	if s == nil || env == nil || env.Type != proto.MsgChat {
		return PrivateGroupMessage{}, false
	}
	payload := &proto.ChatPayload{}
	if err := proto.DecodePayload(env.Payload, payload); err != nil {
		return PrivateGroupMessage{}, false
	}
	key, known := s.groups[payload.ChannelID]
	if !known {
		return PrivateGroupMessage{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(payload.Content)
	if err != nil {
		return PrivateGroupMessage{}, false
	}
	plain, err := gandrcrypto.DecryptGroup(key, payload.ChannelID, raw)
	if err != nil {
		return PrivateGroupMessage{}, false
	}
	return PrivateGroupMessage{
		Hash: env.ContentID(), GroupID: payload.ChannelID, Sender: env.Sender,
		Content: string(plain), At: env.Timestamp,
	}, true
}

// SaveReceivedPrivateGroupMessage persists an already-decrypted incoming
// group message (see DecryptGroupMessage) to the local encrypted DB, the
// same store SendPrivateGroup writes its own outgoing messages to —
// duplicates (by content hash) are silently ignored.
func (s *Session) SaveReceivedPrivateGroupMessage(m PrivateGroupMessage) error {
	if s == nil || s.db == nil {
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	key, ok := s.groups[m.GroupID]
	if !ok {
		return errors.New("gruppen är låst")
	}
	blob, err := gandrcrypto.EncryptGroup(key, m.GroupID, []byte(m.Content))
	if err != nil {
		return err
	}
	return s.db.SavePrivateGroupMessage(gandrclientdb.PrivateGroupMessage{
		Hash: m.Hash, GroupID: m.GroupID, Sender: m.Sender, Ciphertext: blob, MessageAt: m.At,
	})
}

// PublicKey returns this session's own Gandr identity public key, e.g.
// to tell "my own message" apart from someone else's when both carry a
// real sender pubkey (private group messages, unlike public channel
// messages, never use an all-zero sentinel for "you").
func (s *Session) PublicKey() [32]byte {
	if s == nil || s.id == nil {
		return [32]byte{}
	}
	var pub [32]byte
	copy(pub[:], s.id.PublicKey)
	return pub
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
		return errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	return s.db.Block(pubkey, reason)
}

// ConnectPeer asks the local gandrd daemon to dial and federate with
// another node's Yggdrasil transport key (not its GANDR identity key — the
// two are always separate, see gandrd's own startup log:
// "gandrd: yggdrasil node key: <hex>"). Both nodes only need to be
// reachable on the wider Yggdrasil overlay — no direct IP/port between the
// two users is required, the same way two Tor or I2P nodes don't need one.
// This wraps an IPC request gandrd already implements and tests
// end-to-end; it isn't new protocol behavior, just a BACKFLASH-side path
// to trigger it, which was previously missing entirely.
func (s *Session) ConnectPeer(ctx context.Context, yggKeyHex string) error {
	// Validate the input first: a typo'd key should be reported immediately
	// regardless of connection state, rather than being masked by "gandrd
	// isn't running" when the daemon isn't even the problem.
	key, err := parseYggKey(yggKeyHex)
	if err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return errors.New("E2E-CHATT-sessionen är inte ansluten till gandrd")
	}
	return s.client.Connect(ctx, key)
}

func parseYggKey(hexKey string) ([32]byte, error) {
	var key [32]byte
	hexKey = strings.TrimSpace(hexKey)
	if len(hexKey) != 64 {
		return key, errors.New("yggdrasil-nyckeln ska vara 64 hextecken")
	}
	decoded, err := hex.DecodeString(hexKey)
	if err != nil {
		return key, fmt.Errorf("ogiltig yggdrasil-nyckel: %w", err)
	}
	copy(key[:], decoded)
	return key, nil
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
		return Message{}, errors.New("E2E-CHATT-paketet är inte ett chattmeddelande")
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
