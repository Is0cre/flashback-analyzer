package gandr

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// GroupInvite is the decoded form of a group invite string. Unlike GANDR's
// contact invitation (which only conveys a public key), this contains the
// group's password: joining a private group requires both the ID and the
// password, so an invite that only carried the ID would not actually be
// enough to get in. Whoever receives this string can read and post in the
// group — share it the same way you would share the password itself, never
// over a public/unencrypted channel.
type GroupInvite struct {
	Version  int    `json:"version"`
	GroupID  string `json:"group_id"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

const groupInvitePrefix = "BFG1."

// EncodeGroupInvite packages a private group's ID, name and password into a
// single shareable string, so joining it doesn't require manually copying a
// 64-character hex ID and separately relaying the password out of band.
func EncodeGroupInvite(id [32]byte, name, password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("gruppen saknar lösenord")
	}
	payload, err := json.Marshal(GroupInvite{Version: 1, GroupID: hex.EncodeToString(id[:]), Name: name, Password: password})
	if err != nil {
		return "", err
	}
	return groupInvitePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeGroupInvite is the inverse of EncodeGroupInvite.
func DecodeGroupInvite(invite string) (id [32]byte, name, password string, err error) {
	invite = strings.TrimSpace(invite)
	if !strings.HasPrefix(invite, groupInvitePrefix) {
		return id, "", "", errors.New("okänt inbjudningsformat")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(invite, groupInvitePrefix))
	if err != nil {
		return id, "", "", err
	}
	var parsed GroupInvite
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return id, "", "", err
	}
	decoded, err := hex.DecodeString(parsed.GroupID)
	if err != nil || len(decoded) != 32 {
		return id, "", "", errors.New("ogiltigt grupp-ID i inbjudan")
	}
	copy(id[:], decoded)
	if strings.TrimSpace(parsed.Password) == "" {
		return id, "", "", errors.New("inbjudan saknar lösenord")
	}
	return id, parsed.Name, parsed.Password, nil
}

// IsGroupInvite reports whether value looks like a group invite string,
// letting callers distinguish it from a raw hex group ID.
func IsGroupInvite(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), groupInvitePrefix)
}
