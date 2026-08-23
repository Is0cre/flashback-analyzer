package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
)

type SearchService struct {
	Store  *store.Store
	Client *flashback.Client
}

func (s SearchService) Remote(ctx context.Context, query string, page int) ([]flashback.SearchResult, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ingen nätverksklient är konfigurerad")
	}
	return s.Client.Search(ctx, query, page)
}

func (s SearchService) Local(query, threadID string, limit int) ([]flashback.SearchResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.Store.DB.Query(`SELECT post_id,thread_id,author,snippet(post_search,3,'','…',12) FROM post_search WHERE post_search MATCH ? AND (?='' OR thread_id=?) LIMIT ?`, query, threadID, threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []flashback.SearchResult
	for rows.Next() {
		var r flashback.SearchResult
		if err := rows.Scan(&r.PostID, &r.ThreadID, &r.Author, &r.Snippet); err != nil {
			return nil, err
		}
		r.ResultType = "post"
		result = append(result, r)
	}
	return result, rows.Err()
}

func SearchResultRows(rows *sql.Rows) ([]flashback.SearchResult, error) {
	defer rows.Close()
	var out []flashback.SearchResult
	for rows.Next() {
		var r flashback.SearchResult
		if err := rows.Scan(&r.PostID, &r.ThreadID, &r.Author, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
