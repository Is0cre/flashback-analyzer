package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
)

func BenchmarkDashboardSnapshotCached(b *testing.B) {
	s, err := store.Open(filepath.Join(b.TempDir(), "backflash.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.SaveForums([]flashback.ForumNode{{ID: "f1", Title: "Test", URL: "https://www.flashback.org/f1"}}); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := s.SavePage(flashback.ParsedPage{ThreadID: "t1", Title: "Tråd", SourceURL: "https://www.flashback.org/t1", Posts: []flashback.Post{{ID: "p" + number(i), ThreadID: "t1", Author: "test", Timestamp: time.Now(), Text: "text", RawText: "text"}}}); err != nil {
			b.Fatal(err)
		}
	}
	service := &DashboardService{Store: s, Now: time.Now}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := service.Snapshot(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func number(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	return string(digits[i/10%10]) + string(digits[i%10])
}
