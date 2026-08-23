package external

import (
	"crypto/sha256"
	"encoding/hex"
)

func ContentHash(e ExternalEvent) string {
	h := sha256.Sum256([]byte(e.ExternalID + "\x00" + e.Timestamp.String() + "\x00" + e.Title + "\x00" + e.Summary + "\x00" + e.LocationName))
	return hex.EncodeToString(h[:])
}
