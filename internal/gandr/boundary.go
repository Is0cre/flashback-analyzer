// Package gandr defines BACKFLASH's narrow integration boundary to GANDR.
//
// GANDR remains a separate product and Go module. This package intentionally
// contains no identity, vault, client-database, messaging, or federation code.
package gandr

// State describes the private GANDR subsystem from BACKFLASH's perspective.
type State string

const (
	Locked State = "LÅST"
)

// Summary is safe to show in a public BACKFLASH dashboard. It deliberately
// contains no private message data, petnames, identity keys, or user records.
type Summary struct {
	State State
}

// Subsystem is the lazy integration boundary. It starts locked and does not
// touch GANDR storage until an explicit unlock adapter is introduced.
type Subsystem struct {
	summary Summary
}

// New returns a locked GANDR boundary.
//
// PRIVACY INVARIANT: BACKFLASH startup must not open GANDR's encrypted client
// database or identity key. This protects private state from accidental
// exposure and keeps BACKFLASH cookies, Flashback usernames, and reader state
// outside the GANDR security domain. Changing this to auto-unlock would break
// that separation.
func New() *Subsystem {
	return &Subsystem{summary: Summary{State: Locked}}
}

// Summary returns the public, non-sensitive status of the subsystem.
func (s *Subsystem) Summary() Summary {
	if s == nil {
		return Summary{State: Locked}
	}
	return s.summary
}
