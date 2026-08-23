package diagnostics

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

var sequence uint64

// Start enables low-overhead startup/query timing only when explicitly
// requested. It never writes forum content, cookies or search terms.
func Start(label string) func() {
	if os.Getenv("BACKFLASH_PROFILE") != "1" {
		return func() {}
	}
	started := time.Now()
	id := atomic.AddUint64(&sequence, 1)
	return func() { fmt.Fprintf(os.Stderr, "BACKFLASH PROFIL #%d %-28s %s\n", id, label, time.Since(started)) }
}
