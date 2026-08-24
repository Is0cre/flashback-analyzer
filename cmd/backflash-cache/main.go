// Command backflash-cache runs the optional, headless public cache peer.
// It deliberately does not open the TUI, Flashback session or Gandr state.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/backflash-cli/backflash/internal/cacheui"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
	"github.com/backflash-cli/backflash/internal/mesh/ygg"
	"github.com/backflash-cli/backflash/internal/service"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Printf("BACKFLASH CACHE %s\ncommit: %s\nbyggd: %s\ngo: %s\nos/arkitektur: %s/%s\n", version, commit, built, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return
		case "identity":
			printIdentity()
			return
		case "get":
			getObject(os.Args[2:])
			return
		case "tui":
			runTUI()
			return
		case "invite":
			printInvite()
			return
		}
	}
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
	if cfg.OriginEnabled {
		if len(cfg.OriginForums) == 0 {
			fmt.Println("Origin-sync: PÅ men inga forum-URL:er konfigurerade")
		} else {
			läge := "seed-forum"
			if cfg.OriginDiscoverSubforums {
				läge = fmt.Sprintf("underforum · max %d · batch %d", cfg.OriginMaxForums, cfg.OriginBatchSize)
			}
			fmt.Printf("Origin-sync: PÅ · %d seed · %s · intervall %s · första sidan\n", len(cfg.OriginForums), läge, cfg.OriginInterval)
			go originLoop(ctx, cfg, runtime)
		}
	}
	<-ctx.Done()
}

var version = "0.1.0"
var commit = "dev"
var built = "okänt"

func runTUI() {
	cfg := mesh.Load()
	if !cfg.Enabled {
		fatal("mesh är avstängt; sätt [mesh].enabled = true i konfigurationen")
	}
	runtime := meshruntime.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Start(ctx); err != nil {
		fatal("kunde inte starta mesh: %v", err)
	}
	defer runtime.Stop()
	if err := cacheui.Run(runtime, cfg); err != nil {
		fatal("cache-TUI avslutades med fel: %v", err)
	}
}

func printInvite() {
	cfg := mesh.Load()
	identity, err := mesh.LoadIdentity(cfg.IdentityPath)
	if err != nil {
		fatal("kunde inte läsa mesh-identiteten: %v · starta cache-peeren först", err)
	}
	transport, err := ygg.New(ygg.Config{PrivateKey: identity})
	if err != nil {
		fatal("kunde inte beräkna Yggdrasil-nyckeln: %v", err)
	}
	defer transport.Close()
	endpoints := cfg.Advertise
	if len(endpoints) == 0 {
		endpoints = cfg.Listen
	}
	invite, err := mesh.EncodeInvite(transport.PublicKey(), endpoints)
	if err != nil {
		fatal("kunde inte skapa inbjudan: %v", err)
	}
	fmt.Println(invite)
}

// getObject is a deliberately small diagnostic command for a real mesh test.
// It asks the configured peer for one public object and prints its provenance;
// it does not read Flashback, cookies, Gandr state or local SQLite content.
func getObject(args []string) {
	if len(args) != 3 {
		fatal("användning: backflash-cache get SOURCE RESOURCE_ID OBJECT_TYPE")
	}
	cfg := mesh.Load()
	if !cfg.Enabled {
		fatal("mesh är avstängt; sätt [mesh].enabled = true i konfigurationen")
	}
	runtime := meshruntime.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Start(ctx); err != nil {
		fatal("kunde inte starta mesh: %v", err)
	}
	defer runtime.Stop()
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	object, err := runtime.GetResource(requestCtx, args[0], args[1], mesh.ObjectType(args[2]))
	if err != nil {
		fatal("cacheobjekt kunde inte hämtas: %v", err)
	}
	fmt.Printf("hash: %s\nprovenans: %s\nkälla: %s\nresurs: %s\nstorlek: %d byte\n", object.HashString(), object.Provenance, object.Source, object.ResourceID, len(object.Payload))
}

func originLoop(ctx context.Context, cfg mesh.Config, runtime *meshruntime.Runtime) {
	syncer := &service.OriginSync{
		Client:            flashback.NewClient(flashback.AnonymousSession{}),
		Runtime:           runtime,
		Forums:            cfg.OriginForums,
		MaxPages:          cfg.OriginMaxPages,
		DiscoverSubforums: cfg.OriginDiscoverSubforums,
		MaxForums:         cfg.OriginMaxForums,
		BatchSize:         cfg.OriginBatchSize,
	}
	refresh := func() {
		count, err := syncer.SyncOnce(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backflash-cache: origin-sync misslyckades: %v\n", err)
			return
		}
		fmt.Printf("Origin-sync: %d forum-snapshot sparade\n", count)
	}
	refresh()
	ticker := time.NewTicker(cfg.OriginInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func printIdentity() {
	cfg := mesh.Load()
	identity, err := mesh.LoadIdentity(cfg.IdentityPath)
	if err != nil {
		fatal("kunde inte läsa mesh-identiteten: %v", err)
	}
	// peer_key must contain Yggdrasil's derived overlay public key, not the
	// Ed25519 public half of the persisted seed. Constructing the transport
	// without listeners or peers is side-effect free and gives us the exact
	// key used by the running cache process.
	transport, err := ygg.New(ygg.Config{PrivateKey: identity})
	if err != nil {
		fatal("kunde inte beräkna Yggdrasil-nyckeln: %v", err)
	}
	defer transport.Close()
	fmt.Println(transport.PublicKeyString())
	fmt.Printf("overlay: %s\n", transport.OverlayAddressString())
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "backflash-cache: "+format+"\n", args...)
	os.Exit(1)
}
