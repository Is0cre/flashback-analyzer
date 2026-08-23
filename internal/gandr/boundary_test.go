package gandr

import (
	"errors"
	"os"
	"testing"

	gandridentity "github.com/gandr-net/gandr/pkg/identity"
)

func TestMissingVaultCanBeCreatedWithoutOverwriting(t *testing.T) {
	path := t.TempDir() + "/identity.key"
	subsystem := NewAt(path)
	if subsystem.HasVault() {
		t.Fatal("nytt GANDR-valv rapporterades som befintligt")
	}
	if err := subsystem.Unlock("hemligt"); !errors.Is(err, gandridentity.ErrNoKeyfile) {
		t.Fatalf("förväntade saknat valv, fick %v", err)
	}
	if got := subsystem.Summary().State; got != Missing {
		t.Fatalf("fel status för saknat valv: %q", got)
	}
	if err := subsystem.Create("hemligt"); err != nil {
		t.Fatal(err)
	}
	if !subsystem.HasVault() || subsystem.Summary().State != Unlocked {
		t.Fatalf("valvet skapades inte korrekt: %#v", subsystem.Summary())
	}
	if err := subsystem.Create("annat"); err == nil {
		t.Fatal("ett befintligt valv skrevs över")
	}
}

func TestDeleteVaultRemovesOnlyGandrFiles(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/identity.key"
	subsystem := NewAt(path)
	if err := subsystem.Create("hemligt"); err != nil {
		t.Fatal(err)
	}
	subsystem.Lock()
	if err := subsystem.DeleteVault(); err != nil {
		t.Fatal(err)
	}
	if subsystem.HasVault() {
		t.Fatal("GANDR-valvet finns kvar efter radering")
	}
	if _, err := os.Stat(dir + "/client.db"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GANDR client.db finns kvar: %v", err)
	}
}

func TestUnlockUsesGandrKeyfileAndKeepsBoundarySeparate(t *testing.T) {
	path := t.TempDir() + "/identity.key"
	id, err := gandridentity.Generate("")
	if err != nil {
		t.Fatal(err)
	}
	if err := id.Save(path, []byte("hemligt")); err != nil {
		t.Fatal(err)
	}

	subsystem := NewAt(path)
	if got := subsystem.Summary().State; got != Locked {
		t.Fatalf("GANDR startade inte låst: %q", got)
	}
	if err := subsystem.Unlock("fel"); err == nil {
		t.Fatal("fel lösenord accepterades")
	}
	if got := subsystem.Summary().State; got != UnlockErr {
		t.Fatalf("fel upplåsningsstatus: %q", got)
	}
	if err := subsystem.Unlock("hemligt"); err != nil {
		t.Fatal(err)
	}
	summary := subsystem.Summary()
	if summary.State != Unlocked || summary.Fingerprint == "" {
		t.Fatalf("GANDR låstes inte upp korrekt: %#v", summary)
	}
	subsystem.Lock()
	if got := subsystem.Summary().State; got != Locked {
		t.Fatalf("GANDR låstes inte igen: %q", got)
	}
}
