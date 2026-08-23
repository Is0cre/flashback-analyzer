from __future__ import annotations

import hashlib
import sqlite3
from pathlib import Path

from .models import ParsedPage
from .navigation import ForumNode, ThreadSummary
from .urls import normalize_url, thread_page_url, url_domain

SCHEMA_VERSION = 10

SCHEMA = """
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS threads (
    thread_id INTEGER PRIMARY KEY, title TEXT, url TEXT, forum_name TEXT,
    first_seen_at TEXT, last_fetched_at TEXT, first_post_at TEXT, last_post_at TEXT,
    page_count INTEGER NOT NULL DEFAULT 1, post_count INTEGER NOT NULL DEFAULT 0,
    last_page_seen INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS users (user_id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS posts (
    post_id INTEGER PRIMARY KEY, thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    page INTEGER NOT NULL, position_on_page INTEGER, ordinal INTEGER,
    user_id INTEGER NOT NULL REFERENCES users(user_id), posted_at TEXT, text TEXT NOT NULL,
    raw_text TEXT NOT NULL, source_url TEXT, content_hash TEXT, ingested_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_posts_thread ON posts(thread_id);
CREATE INDEX IF NOT EXISTS idx_posts_user ON posts(user_id);
CREATE INDEX IF NOT EXISTS idx_posts_posted_at ON posts(posted_at);
CREATE VIRTUAL TABLE IF NOT EXISTS post_search USING fts5(
    post_id UNINDEXED, thread_id UNINDEXED, username, text
);
CREATE TABLE IF NOT EXISTS raw_pages (
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE, page INTEGER NOT NULL,
    source_url TEXT NOT NULL, content_hash TEXT NOT NULL, raw_html TEXT NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(thread_id, page)
);
CREATE TABLE IF NOT EXISTS quotes (
    quote_id INTEGER PRIMARY KEY AUTOINCREMENT, post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    quoted_post_id INTEGER, quoted_author TEXT, quote_text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_quotes_post ON quotes(post_id);
CREATE INDEX IF NOT EXISTS idx_quotes_quoted_post ON quotes(quoted_post_id);
CREATE TABLE IF NOT EXISTS links (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE, url TEXT NOT NULL,
    domain TEXT NOT NULL DEFAULT '', author TEXT, posted_at TEXT, PRIMARY KEY(post_id, url)
);
CREATE INDEX IF NOT EXISTS idx_links_domain ON links(domain);
CREATE TABLE IF NOT EXISTS analysis_metadata (
    analysis_id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
    model TEXT NOT NULL, prompt_version TEXT NOT NULL, analysis_version TEXT NOT NULL, input_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(entity_type, entity_id, model, prompt_version, analysis_version, input_hash)
);
CREATE TABLE IF NOT EXISTS questions (
    question_id INTEGER PRIMARY KEY AUTOINCREMENT, thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    question TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS stances (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    question_id INTEGER NOT NULL REFERENCES questions(question_id) ON DELETE CASCADE,
    stance TEXT NOT NULL CHECK(stance IN ('strong_yes','probably_yes','uncertain','probably_no','strong_no','irrelevant','unclear')),
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1), model TEXT NOT NULL, rationale TEXT,
    prompt_version TEXT, analysis_version TEXT, input_hash TEXT, PRIMARY KEY(post_id, question_id, model)
);
CREATE TABLE IF NOT EXISTS segments (
    segment_id INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    segment_number INTEGER NOT NULL,
    first_post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    last_post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    start_time TEXT,
    end_time TEXT,
    post_count INTEGER NOT NULL,
    summary TEXT,
    segmentation_version TEXT NOT NULL,
    UNIQUE(thread_id, segment_number)
);
CREATE TABLE IF NOT EXISTS segment_posts (
    segment_id INTEGER NOT NULL REFERENCES segments(segment_id) ON DELETE CASCADE,
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    PRIMARY KEY(segment_id, post_id)
);
CREATE INDEX IF NOT EXISTS idx_segments_thread ON segments(thread_id);
CREATE INDEX IF NOT EXISTS idx_segment_posts_post ON segment_posts(post_id);
CREATE TABLE IF NOT EXISTS topics (
    topic_id INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    method TEXT NOT NULL,
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    input_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(thread_id, label, method)
);
CREATE TABLE IF NOT EXISTS post_topics (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    topic_id INTEGER NOT NULL REFERENCES topics(topic_id) ON DELETE CASCADE,
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    method TEXT NOT NULL,
    PRIMARY KEY(post_id, topic_id, method)
);
CREATE INDEX IF NOT EXISTS idx_topics_thread ON topics(thread_id);
CREATE INDEX IF NOT EXISTS idx_post_topics_topic ON post_topics(topic_id);
CREATE TABLE IF NOT EXISTS question_options (
    option_id INTEGER PRIMARY KEY AUTOINCREMENT,
    question_id INTEGER NOT NULL REFERENCES questions(question_id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    method TEXT NOT NULL,
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    UNIQUE(question_id, label, method)
);
CREATE TABLE IF NOT EXISTS post_questions (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    question_id INTEGER NOT NULL REFERENCES questions(question_id) ON DELETE CASCADE,
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    method TEXT NOT NULL,
    PRIMARY KEY(post_id, question_id, method)
);
CREATE INDEX IF NOT EXISTS idx_questions_thread ON questions(thread_id);
CREATE INDEX IF NOT EXISTS idx_post_questions_question ON post_questions(question_id);
CREATE TABLE IF NOT EXISTS discovery_items (
    feed TEXT NOT NULL,
    thread_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    views INTEGER,
    readers INTEGER,
    replies INTEGER,
    source_url TEXT NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(feed, thread_id)
);
CREATE INDEX IF NOT EXISTS idx_discovery_items_fetched ON discovery_items(fetched_at);
CREATE TABLE IF NOT EXISTS tracked_threads (
    thread_id INTEGER PRIMARY KEY REFERENCES threads(thread_id) ON DELETE CASCADE,
    added_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS reader_state (
    thread_id INTEGER PRIMARY KEY REFERENCES threads(thread_id) ON DELETE CASCADE,
    last_seen_post_id INTEGER,
    last_seen_at TEXT
);
CREATE TABLE IF NOT EXISTS forum_sections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    forum_id TEXT,
    parent_id INTEGER REFERENCES forum_sections(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_browsable INTEGER NOT NULL DEFAULT 0,
    last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source, url)
);
CREATE INDEX IF NOT EXISTS idx_forum_sections_parent ON forum_sections(parent_id, sort_order);
CREATE TABLE IF NOT EXISTS forum_threads (
    forum_id INTEGER NOT NULL REFERENCES forum_sections(id) ON DELETE CASCADE,
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    is_sticky INTEGER NOT NULL DEFAULT 0,
    author TEXT,
    reply_count INTEGER,
    view_count INTEGER,
    last_post_at TEXT,
    last_post_author TEXT,
    page_count INTEGER,
    last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(forum_id, thread_id)
);
CREATE INDEX IF NOT EXISTS idx_forum_threads_thread ON forum_threads(thread_id);
CREATE TABLE IF NOT EXISTS navigation_cache_metadata (
    source TEXT PRIMARY KEY,
    cache_key TEXT NOT NULL,
    source_url TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
"""


class Database:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.conn = sqlite3.connect(path)
        self.conn.row_factory = sqlite3.Row
        self.conn.executescript(SCHEMA)
        self._migrate()

    def _migrate(self) -> None:
        migrations = {
            "threads": {"url": "TEXT", "forum_name": "TEXT", "first_seen_at": "TEXT", "last_fetched_at": "TEXT", "first_post_at": "TEXT", "last_post_at": "TEXT", "page_count": "INTEGER NOT NULL DEFAULT 1", "post_count": "INTEGER NOT NULL DEFAULT 0"},
            "posts": {"position_on_page": "INTEGER", "source_url": "TEXT", "content_hash": "TEXT"},
            "links": {"domain": "TEXT NOT NULL DEFAULT ''", "author": "TEXT", "posted_at": "TEXT"},
            "stances": {"prompt_version": "TEXT", "analysis_version": "TEXT", "input_hash": "TEXT"},
            "questions": {"method": "TEXT NOT NULL DEFAULT 'manual'", "confidence": "REAL NOT NULL DEFAULT 1.0", "input_hash": "TEXT"},
            "forum_threads": {"author": "TEXT", "reply_count": "INTEGER", "view_count": "INTEGER", "last_post_at": "TEXT", "last_post_author": "TEXT", "page_count": "INTEGER"},
        }
        for table, columns in migrations.items():
            existing = {row[1] for row in self.conn.execute(f"PRAGMA table_info({table})")}
            for column, definition in columns.items():
                if column not in existing:
                    self.conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")
        row = self.conn.execute("SELECT version FROM schema_version LIMIT 1").fetchone()
        if row is None:
            self.conn.execute("INSERT INTO schema_version(version) VALUES (?)", (SCHEMA_VERSION,))
        elif int(row[0]) < SCHEMA_VERSION:
            self.conn.execute("UPDATE schema_version SET version=?", (SCHEMA_VERSION,))
        self.conn.commit()
        self.conn.execute("INSERT OR IGNORE INTO tracked_threads(thread_id) SELECT thread_id FROM threads")
        self.conn.execute("""INSERT INTO post_search(post_id, thread_id, username, text)
            SELECT p.post_id, p.thread_id, u.username, p.text FROM posts p JOIN users u ON u.user_id=p.user_id
            WHERE NOT EXISTS (SELECT 1 FROM post_search s WHERE s.post_id=CAST(p.post_id AS TEXT))""")
        self.conn.commit()

    def close(self) -> None:
        self.conn.close()

    def __enter__(self) -> "Database":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def store_page(self, parsed: ParsedPage) -> int:
        cur = self.conn.cursor()
        source_url = parsed.source_url or thread_page_url(parsed.thread_id, parsed.page)
        cur.execute("""INSERT INTO threads(thread_id, title, url, forum_name, last_page_seen, page_count, first_seen_at)
            VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP) ON CONFLICT(thread_id) DO UPDATE SET
            title=COALESCE(excluded.title, threads.title), url=COALESCE(excluded.url, threads.url),
            forum_name=COALESCE(excluded.forum_name, threads.forum_name),
            last_page_seen=MAX(threads.last_page_seen, excluded.last_page_seen), page_count=MAX(threads.page_count, excluded.page_count),
            updated_at=CURRENT_TIMESTAMP""", (parsed.thread_id, parsed.title, f"https://www.flashback.org/t{parsed.thread_id}", parsed.forum_name, parsed.max_page, parsed.max_page))
        cur.execute("INSERT OR IGNORE INTO tracked_threads(thread_id) VALUES (?)", (parsed.thread_id,))
        if parsed.raw_html is not None:
            cur.execute("""INSERT INTO raw_pages(thread_id, page, source_url, content_hash, raw_html) VALUES (?, ?, ?, ?, ?)
                ON CONFLICT(thread_id, page) DO UPDATE SET source_url=excluded.source_url, content_hash=excluded.content_hash,
                raw_html=excluded.raw_html, fetched_at=CURRENT_TIMESTAMP""", (parsed.thread_id, parsed.page, source_url, hashlib.sha256(parsed.raw_html.encode()).hexdigest(), parsed.raw_html))
        for position, post in enumerate(parsed.posts, start=1):
            cur.execute("INSERT OR IGNORE INTO users(username) VALUES (?)", (post.author,))
            user_id = cur.execute("SELECT user_id FROM users WHERE username=?", (post.author,)).fetchone()[0]
            posted_at = post.posted_at.isoformat() if post.posted_at else None
            cur.execute("""INSERT INTO posts(post_id, thread_id, page, position_on_page, ordinal, user_id, posted_at, text, raw_text, source_url, content_hash)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(post_id) DO UPDATE SET page=excluded.page,
                position_on_page=excluded.position_on_page, ordinal=excluded.ordinal, user_id=excluded.user_id,
                posted_at=excluded.posted_at, text=excluded.text, raw_text=excluded.raw_text, source_url=excluded.source_url,
                content_hash=excluded.content_hash""", (post.post_id, post.thread_id, post.page, position, post.ordinal, user_id, posted_at, post.text, post.raw_text, source_url, hashlib.sha256(post.raw_text.encode()).hexdigest()))
            cur.execute("DELETE FROM links WHERE post_id=?", (post.post_id,))
            cur.execute("DELETE FROM post_search WHERE post_id=?", (str(post.post_id),))
            cur.execute("INSERT INTO post_search(post_id, thread_id, username, text) VALUES (?, ?, ?, ?)",
                        (post.post_id, post.thread_id, post.author, post.text))
            cur.executemany("INSERT OR IGNORE INTO links(post_id, url, domain, author, posted_at) VALUES (?, ?, ?, ?, ?)", [(post.post_id, normalize_url(url), url_domain(url), post.author, posted_at) for url in post.links])
        # Resolve after all posts on this page exist, including forward references.
        for post in parsed.posts:
            cur.execute("DELETE FROM quotes WHERE post_id=?", (post.post_id,))
            for quote in post.quotes:
                quoted_id = self._resolve_quote(cur, post.thread_id, quote.post_id, quote.author, quote.text, post.post_id)
                cur.execute("INSERT INTO quotes(post_id, quoted_post_id, quoted_author, quote_text) VALUES (?, ?, ?, ?)", (post.post_id, quoted_id, quote.author, quote.text))
        cur.execute("""UPDATE threads SET last_fetched_at=CURRENT_TIMESTAMP,
            post_count=(SELECT COUNT(*) FROM posts WHERE thread_id=?), first_post_at=(SELECT MIN(posted_at) FROM posts WHERE thread_id=?),
            last_post_at=(SELECT MAX(posted_at) FROM posts WHERE thread_id=?) WHERE thread_id=?""", (parsed.thread_id, parsed.thread_id, parsed.thread_id, parsed.thread_id))
        self.conn.commit()
        return len(parsed.posts)

    @staticmethod
    def _resolve_quote(cur: sqlite3.Cursor, thread_id: int, post_id: int | None, author: str | None, quote_text: str, source_post_id: int) -> int | None:
        if post_id is not None and cur.execute("SELECT 1 FROM posts WHERE post_id=? AND thread_id=?", (post_id, thread_id)).fetchone():
            return post_id
        if author:
            row = cur.execute("""SELECT p.post_id FROM posts p JOIN users u ON u.user_id=p.user_id
                WHERE p.thread_id=? AND u.username=? AND p.post_id<>? AND (p.text LIKE ? OR p.raw_text LIKE ?) ORDER BY p.post_id LIMIT 1""", (thread_id, author, source_post_id, f"%{quote_text}%", f"%{quote_text}%")).fetchone()
            if row:
                return int(row[0])
        return None

    def thread_summary(self, thread_id: int) -> dict[str, object]:
        thread = self.conn.execute("SELECT * FROM threads WHERE thread_id=?", (thread_id,)).fetchone()
        if not thread:
            raise KeyError(thread_id)
        totals = self.conn.execute("SELECT COUNT(*) AS posts, COUNT(DISTINCT user_id) AS users, MIN(posted_at) AS first_post, MAX(posted_at) AS last_post FROM posts WHERE thread_id=?", (thread_id,)).fetchone()
        top_users = self.conn.execute("""SELECT u.username, COUNT(*) AS posts FROM posts p JOIN users u ON u.user_id=p.user_id
            WHERE p.thread_id=? GROUP BY p.user_id ORDER BY posts DESC, u.username LIMIT 10""", (thread_id,)).fetchall()
        post_count = int(totals["posts"] or 0)
        return {"thread_id": thread_id, "title": thread["title"], "url": thread["url"], "forum_name": thread["forum_name"], "first_seen_at": thread["first_seen_at"], "last_fetched_at": thread["last_fetched_at"], "page_count": thread["page_count"], "last_page_seen": thread["last_page_seen"], "posts": post_count, "users": int(totals["users"] or 0), "first_post": totals["first_post"], "last_post": totals["last_post"], "top_users": [dict(row) for row in top_users], "top10_share": (sum(row["posts"] for row in top_users) / post_count) if post_count else 0.0}

    def link_statistics(self, thread_id: int) -> list[dict[str, object]]:
        rows = self.conn.execute("""SELECT l.domain, COUNT(*) AS links, COUNT(DISTINCT l.url) AS unique_urls,
            COUNT(DISTINCT p.user_id) AS unique_users FROM links l JOIN posts p ON p.post_id=l.post_id
            WHERE p.thread_id=? GROUP BY l.domain ORDER BY links DESC, l.domain""", (thread_id,)).fetchall()
        return [dict(row) for row in rows]

    def store_discovery_items(self, items: list[object]) -> int:
        """Persist the latest live-feed candidates for offline TUI startup."""
        stored = 0
        for item in items:
            self.conn.execute("""INSERT INTO discovery_items(feed, thread_id, title, views, readers, replies, source_url)
                VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(feed, thread_id) DO UPDATE SET title=excluded.title,
                views=excluded.views, readers=excluded.readers, replies=excluded.replies,
                source_url=excluded.source_url, fetched_at=CURRENT_TIMESTAMP""",
                (item.feed, item.thread_id, item.title, item.views, item.readers, item.replies, item.url))
            stored += 1
        self.conn.commit()
        return stored

    def cached_discovery_items(self) -> list[object]:
        """Return the most recently cached candidate for each discovered thread."""
        from .discovery import DiscoveryItem

        rows = self.conn.execute("""SELECT d.feed, d.thread_id, d.title, d.views, d.readers, d.replies
            FROM discovery_items d JOIN (SELECT thread_id, MAX(fetched_at) AS fetched_at
            FROM discovery_items GROUP BY thread_id) latest ON latest.thread_id=d.thread_id AND latest.fetched_at=d.fetched_at
            ORDER BY d.feed, d.replies DESC, d.readers DESC, d.thread_id""").fetchall()
        return [DiscoveryItem(row["feed"], row["thread_id"], row["title"], row["views"], row["readers"], row["replies"]) for row in rows]

    def tracked_thread_rows(self) -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT t.thread_id, t.title, t.forum_name, t.post_count, t.last_fetched_at,
            (SELECT COUNT(*) FROM posts p2 WHERE p2.thread_id=t.thread_id AND
                (rs.last_seen_post_id IS NULL OR p2.post_id > rs.last_seen_post_id)) AS unread_count,
            rs.last_seen_post_id FROM tracked_threads tt JOIN threads t ON t.thread_id=tt.thread_id
            LEFT JOIN reader_state rs ON rs.thread_id=t.thread_id ORDER BY t.last_fetched_at DESC, t.thread_id""").fetchall()

    def thread_posts(self, thread_id: int) -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT p.post_id, p.ordinal, p.page, p.position_on_page, p.posted_at,
            p.text, p.raw_text, u.username FROM posts p JOIN users u ON u.user_id=p.user_id
            WHERE p.thread_id=? ORDER BY COALESCE(p.posted_at, ''), COALESCE(p.ordinal, p.post_id), p.post_id""", (thread_id,)).fetchall()

    def search_posts(self, query: str, thread_id: int | None = None, limit: int = 200) -> list[sqlite3.Row]:
        """Search cached original post text and usernames using SQLite FTS5."""

        query = " ".join(query.split()).strip()
        if not query:
            return []
        match = " OR ".join(f'"{term.replace(chr(34), "")}"' for term in query.split())
        params: list[object] = [match]
        thread_clause = ""
        if thread_id is not None:
            thread_clause = " AND s.thread_id=?"
            params.append(thread_id)
        params.append(limit)
        return self.conn.execute(f"""SELECT DISTINCT p.post_id, p.ordinal, p.page, p.position_on_page, p.posted_at,
            p.text, p.raw_text, u.username FROM post_search s
            JOIN posts p ON p.post_id=CAST(s.post_id AS INTEGER)
            JOIN users u ON u.user_id=p.user_id
            WHERE post_search MATCH ?{thread_clause}
            ORDER BY COALESCE(p.posted_at, ''), COALESCE(p.ordinal, p.post_id), p.post_id LIMIT ?""", params).fetchall()

    def post_quotes(self, post_id: int) -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT quoted_post_id, quoted_author, quote_text FROM quotes
            WHERE post_id=? ORDER BY quote_id""", (post_id,)).fetchall()

    def reader_position(self, thread_id: int) -> int | None:
        row = self.conn.execute("SELECT last_seen_post_id FROM reader_state WHERE thread_id=?", (thread_id,)).fetchone()
        return int(row[0]) if row and row[0] is not None else None

    def mark_post_seen(self, thread_id: int, post_id: int) -> None:
        self.conn.execute("""INSERT INTO reader_state(thread_id, last_seen_post_id, last_seen_at) VALUES (?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(thread_id) DO UPDATE SET last_seen_post_id=excluded.last_seen_post_id, last_seen_at=CURRENT_TIMESTAMP""", (thread_id, post_id))
        self.conn.commit()

    def store_forum_nodes(self, nodes: list[ForumNode]) -> int:
        """Upsert a navigation tree, resolving parents by canonical URL."""

        for node in nodes:
            self.conn.execute("""INSERT INTO forum_sections
                (source, title, url, forum_id, sort_order, is_browsable, last_seen_at)
                VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
                ON CONFLICT(source, url) DO UPDATE SET title=excluded.title,
                forum_id=excluded.forum_id, sort_order=excluded.sort_order,
                is_browsable=excluded.is_browsable, last_seen_at=CURRENT_TIMESTAMP""",
                (node.source, node.title, node.url, node.external_id, node.sort_order, int(node.is_browsable)))
        for node in nodes:
            parent_id = None
            if node.parent_url:
                row = self.conn.execute("SELECT id FROM forum_sections WHERE source=? AND url=?", (node.source, node.parent_url)).fetchone()
                parent_id = int(row[0]) if row else None
            self.conn.execute("UPDATE forum_sections SET parent_id=? WHERE source=? AND url=?", (parent_id, node.source, node.url))
        self.conn.commit()
        return len(nodes)

    def replace_forum_nodes(self, nodes: list[ForumNode]) -> int:
        """Replace one source's derived menu without touching scraped data."""

        source = nodes[0].source if nodes else "flashback"
        self.conn.execute("DELETE FROM forum_threads WHERE forum_id IN (SELECT id FROM forum_sections WHERE source=?)", (source,))
        self.conn.execute("DELETE FROM forum_sections WHERE source=?", (source,))
        self.conn.commit()
        return self.store_forum_nodes(nodes)

    def forum_roots(self, source: str = "flashback") -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT * FROM forum_sections
            WHERE source=? AND parent_id IS NULL ORDER BY sort_order, title""", (source,)).fetchall()

    def forum_children(self, section_id: int) -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT * FROM forum_sections
            WHERE parent_id=? ORDER BY sort_order, title""", (section_id,)).fetchall()

    def forum_section(self, section_id: int) -> sqlite3.Row | None:
        return self.conn.execute("SELECT * FROM forum_sections WHERE id=?", (section_id,)).fetchone()

    def store_forum_thread_summaries(self, forum_id: int, summaries: list[ThreadSummary]) -> int:
        """Cache listing rows and create lightweight canonical thread placeholders."""

        for position, summary in enumerate(summaries):
            self.conn.execute("""INSERT INTO threads(thread_id, title, url, page_count)
                VALUES (?, ?, ?, COALESCE(?, 1)) ON CONFLICT(thread_id) DO UPDATE SET
                title=COALESCE(excluded.title, threads.title), url=COALESCE(excluded.url, threads.url),
                page_count=MAX(threads.page_count, excluded.page_count)""",
                (summary.thread_id, summary.title, summary.url, summary.page_count))
            self.conn.execute("""INSERT INTO forum_threads
                (forum_id, thread_id, position, is_sticky, author, reply_count, view_count, last_post_at, last_post_author, page_count, last_seen_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
                ON CONFLICT(forum_id, thread_id) DO UPDATE SET position=excluded.position,
                is_sticky=excluded.is_sticky, author=excluded.author, reply_count=excluded.reply_count,
                view_count=excluded.view_count, last_post_at=excluded.last_post_at,
                last_post_author=excluded.last_post_author, page_count=excluded.page_count,
                last_seen_at=CURRENT_TIMESTAMP""",
                (forum_id, summary.thread_id, position, int(summary.is_sticky), summary.author,
                 summary.reply_count, summary.view_count, summary.last_post_at.isoformat() if summary.last_post_at else None,
                 summary.last_post_author, summary.page_count))
        self.conn.commit()
        return len(summaries)

    def forum_thread_rows(self, forum_id: int) -> list[sqlite3.Row]:
        return self.conn.execute("""SELECT t.*, ft.position, ft.is_sticky, ft.author AS listing_author,
            ft.reply_count, ft.view_count, ft.last_post_at AS listing_last_post_at,
            ft.last_post_author AS listing_last_post_author, ft.page_count AS listing_page_count,
            ft.last_seen_at AS listing_seen_at
            FROM forum_threads ft JOIN threads t ON t.thread_id=ft.thread_id
            WHERE ft.forum_id=? ORDER BY ft.position, t.title""", (forum_id,)).fetchall()

    def navigation_cache(self, source: str = "flashback") -> sqlite3.Row | None:
        return self.conn.execute("SELECT * FROM navigation_cache_metadata WHERE source=?", (source,)).fetchone()

    def set_navigation_cache(self, source: str, cache_key: str, source_url: str, expires_at: str) -> None:
        self.conn.execute("""INSERT INTO navigation_cache_metadata(source, cache_key, source_url, fetched_at, expires_at)
            VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?) ON CONFLICT(source) DO UPDATE SET cache_key=excluded.cache_key,
            source_url=excluded.source_url, fetched_at=CURRENT_TIMESTAMP, expires_at=excluded.expires_at""",
            (source, cache_key, source_url, expires_at))
        self.conn.commit()
