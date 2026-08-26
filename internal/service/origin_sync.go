package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
)

// OriginSync performs a deliberately small, rate-limited refresh of configured
// public Flashback forums. It stores snapshots in the public mesh object store;
// it does not open SQLite, cookies, Gandr state, or reader state.
type OriginSync struct {
	Client            *flashback.Client
	Runtime           *meshruntime.Runtime
	Forums            []string
	Limiter           *external.RateLimiter
	MaxPages          int
	DiscoverSubforums bool
	MaxForums         int
	BatchSize         int
	Now               func() time.Time
	known             map[string]flashback.ForumNode
	queue             []flashback.ForumNode
	started           bool
}

type ForumSnapshot struct {
	Forum     flashback.ForumNode       `json:"forum"`
	Children  []flashback.ForumNode     `json:"children,omitempty"`
	Threads   []flashback.ThreadSummary `json:"threads,omitempty"`
	FetchedAt time.Time                 `json:"fetched_at"`
}

func (s *OriginSync) SyncOnce(ctx context.Context) (int, error) {
	if s == nil || s.Client == nil || s.Runtime == nil {
		return 0, errors.New("origin-sync saknar klient eller mesh-runtime")
	}
	if len(s.Forums) == 0 {
		return 0, errors.New("inga forum-URL:er är konfigurerade för origin-sync")
	}
	if s.Limiter == nil {
		s.Limiter = external.NewRateLimiter()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.DiscoverSubforums {
		return s.syncDiscovered(ctx)
	}
	stored := 0
	for _, rawURL := range s.Forums {
		count, err := s.syncForum(ctx, flashback.ForumNode{ID: flashback.ForumID(rawURL), URL: rawURL, Browsable: true})
		if err != nil {
			return stored, err
		}
		stored += count
	}
	return stored, nil
}

func (s *OriginSync) syncDiscovered(ctx context.Context) (int, error) {
	if !s.started {
		s.known = make(map[string]flashback.ForumNode)
		for _, rawURL := range s.Forums {
			forum := flashback.ForumNode{ID: flashback.ForumID(rawURL), URL: rawURL, Browsable: true}
			s.known[forum.URL] = forum
			s.queue = append(s.queue, forum)
		}
		s.started = true
	}
	if len(s.queue) == 0 {
		forums := make([]flashback.ForumNode, 0, len(s.known))
		for _, forum := range s.known {
			forums = append(forums, forum)
		}
		sort.Slice(forums, func(i, j int) bool { return forums[i].URL < forums[j].URL })
		s.queue = forums
	}
	batch := s.BatchSize
	if batch < 1 {
		batch = 10
	}
	stored := 0
	for len(s.queue) > 0 && stored < batch {
		forum := s.queue[0]
		s.queue = s.queue[1:]
		count, children, err := s.syncForumWithChildren(ctx, forum)
		if err != nil {
			return stored, err
		}
		stored += count
		for _, child := range children {
			if len(s.known) >= s.MaxForums && s.MaxForums > 0 {
				break
			}
			if _, exists := s.known[child.URL]; exists {
				continue
			}
			s.known[child.URL] = child
			s.queue = append(s.queue, child)
		}
	}
	return stored, nil
}

func (s *OriginSync) syncForum(ctx context.Context, forum flashback.ForumNode) (int, error) {
	count, _, err := s.syncForumWithChildren(ctx, forum)
	return count, err
}

func (s *OriginSync) syncForumWithChildren(ctx context.Context, forum flashback.ForumNode) (int, []flashback.ForumNode, error) {
	if err := s.Limiter.Wait(ctx); err != nil {
		return 0, nil, err
	}
	children, threads, err := s.Client.ForumPage(ctx, forum)
	if err != nil {
		return 0, nil, fmt.Errorf("hämta %s: %w", forum.URL, err)
	}
	// MaxPages only extends the thread listing: subforum discovery always
	// comes from page 1's navigation, matching Flashback's own layout.
	for page := 2; page <= s.MaxPages; page++ {
		if err := s.Limiter.Wait(ctx); err != nil {
			return 0, nil, err
		}
		more, err := s.Client.ThreadsPage(ctx, forum, page)
		if err != nil || len(more) == 0 {
			// A short forum has fewer pages than MaxPages; that is not a
			// sync failure, so stop paginating instead of erroring out.
			break
		}
		threads = append(threads, more...)
	}
	now := s.Now()
	payload, err := json.Marshal(ForumSnapshot{Forum: forum, Children: children, Threads: threads, FetchedAt: now})
	if err != nil {
		return 0, nil, err
	}
	object := mesh.NewObject(mesh.ForumSnapshot, "flashback", "forum:"+forum.ID, now, payload, mesh.OriginVerified)
	if err := s.Runtime.PutLocal(object); err != nil {
		return 0, nil, fmt.Errorf("spara forum %s: %w", forum.URL, err)
	}
	return 1, children, nil
}
