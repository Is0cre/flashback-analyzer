// Command backflash-cache runs the optional, headless public cache peer.
// It deliberately does not open the TUI, Flashback session or Gandr state.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
)

func main() {
	cfg := mesh.Load()
	if !cfg.Enabled {
		fatal("mesh är avstängt; sätt [mesh].enabled = true i konfigurationen")
	}
	if len(cfg.Listen) == 0 {
		fatal("ingen mesh-lyssnare konfigurerad; ange [mesh].listen")
	}

	runtime := meshruntime.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Start(ctx); err != nil {
		fatal("kunde inte starta mesh: %v", err)
	}
	defer runtime.Stop()

	snapshot := runtime.Snapshot()
	fmt.Printf("BACKFLASH CACHE · %s · identitet %s · lyssnar på %v\n", snapshot.State, snapshot.Identity, runtime.ListenAddrs())
	if cfg.ShareCache {
		fmt.Println("Delning: PÅ · endast publika, verifierade cacheobjekt")
	} else {
		fmt.Println("Delning: AV · noden hämtar men serverar inte cacheobjekt")
	}
	<-ctx.Done()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "backflash-cache: "+format+"\n", args...)
	os.Exit(1)
}
