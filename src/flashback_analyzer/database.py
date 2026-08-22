from __future__ import annotations

import hashlib
import sqlite3
from pathlib import Path

from .models import ParsedPage
from .urls import normalize_url, thread_page_url, url_domain

SCHEMA_VERSION = 4

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
