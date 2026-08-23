package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
)

func TestDashboardSnapshotUsesLocalDatabaseOnly(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "backflash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	if err := s.SaveForums([]flashback.ForumNode{{ID: "f1", Title: "Brott", URL: "https://www.flashback.org/f1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThreads("f1", []flashback.ThreadSummary{{ID: "t1", Title: "En riktig titel", URL: "https://www.flashback.org/t1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePage(flashback.ParsedPage{ThreadID: "t1", Title: "En riktig titel", Page: 1, MaxPage: 1, Posts: []flashback.Post{{ID: "p1", ThreadID: "t1", Timestamp: now.Add(-10 * time.Minute), Text: "lokalt innehåll", RawText: "lokalt innehåll"}}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (&DashboardService{Store: s, Now: func() time.Time { return now }}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ForumCount != 1 || snapshot.ThreadCount != 1 || snapshot.PostCount != 1 || snapshot.PostsLastHour != 1 {
		t.Fatalf("oväntad dashboard-snapshot: %#v", snapshot)
	}
	if len(snapshot.HotThreads) != 1 || snapshot.HotThreads[0].Title != "En riktig titel" {
		t.Fatalf("saknar het tråd: %#v", snapshot.HotThreads)
	}
}

func TestDashboardDoesNotClaimMeshOnlineWhenOnlyConfigured(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "backflash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snapshot, err := (&DashboardService{Store: s, Now: time.Now, MeshConfigured: true}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mesh != "VALD" {
		t.Fatalf("meshstatus blev %q, väntade VALD", snapshot.Mesh)
	}
}
