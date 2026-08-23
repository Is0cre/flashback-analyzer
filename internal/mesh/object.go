// Package mesh contains the deliberately small public-cache protocol core.
// It has no user identity, cookies, chat, or Gandr dependencies.
package mesh

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type ObjectType string

const (
	ForumSnapshot         ObjectType = "forum_snapshot"
	ThreadListingSnapshot ObjectType = "thread_listing_snapshot"
	ThreadPageSnapshot    ObjectType = "thread_page_snapshot"
)

type Provenance string

const (
	OriginVerified     Provenance = "ORIGIN_VERIFIED"
	LocalObserved      Provenance = "LOCAL_OBSERVED"
	MultiPeerAgreement Provenance = "MULTI_PEER_AGREEMENT"
	PeerOnly           Provenance = "PEER_ONLY"
	Stale              Provenance = "STALE"
)

const MaxObjectSize = 16 << 20

type CacheObject struct {
	Type       ObjectType
	Source     string
	ResourceID string
	FetchedAt  time.Time
	Payload    []byte
	Hash       [32]byte
	Provenance Provenance
}

func NewObject(typ ObjectType, source, resourceID string, fetchedAt time.Time, payload []byte, provenance Provenance) CacheObject {
	return CacheObject{
		Type: typ, Source: source, ResourceID: resourceID, FetchedAt: fetchedAt,
		Payload: append([]byte(nil), payload...), Hash: sha256.Sum256(payload), Provenance: provenance,
	}
}

func (o CacheObject) HashString() string { return hex.EncodeToString(o.Hash[:]) }

func (o CacheObject) Validate() error {
	if o.Type == "" || o.Source == "" || o.ResourceID == "" {
		return errors.New("cacheobjekt saknar typ, källa eller resurs-id")
	}
	if len(o.Payload) == 0 || len(o.Payload) > MaxObjectSize {
		return fmt.Errorf("ogiltig cacheobjektstorlek: %d", len(o.Payload))
	}
	if sha256.Sum256(o.Payload) != o.Hash {
		return errors.New("cacheobjektets hash stämmer inte")
	}
	return nil
}
