from __future__ import annotations

import sqlite3
from pathlib import Path

from .models import ParsedPage

SCHEMA = """
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS threads (
    thread_id INTEGER PRIMARY KEY,
    title TEXT,
    last_page_seen INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    user_id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS posts (
    post_id INTEGER PRIMARY KEY,
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    page INTEGER NOT NULL,
    ordinal INTEGER,
    user_id INTEGER NOT NULL REFERENCES users(user_id),
    posted_at TEXT,
    text TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    ingested_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_posts_thread ON posts(thread_id);
CREATE INDEX IF NOT EXISTS idx_posts_user ON posts(user_id);
CREATE INDEX IF NOT EXISTS idx_posts_posted_at ON posts(posted_at);

CREATE TABLE IF NOT EXISTS quotes (
    quote_id INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    quoted_post_id INTEGER,
    quoted_author TEXT,
    quote_text TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_quotes_post ON quotes(post_id);

CREATE TABLE IF NOT EXISTS links (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    PRIMARY KEY(post_id, url)
);

-- Analysis tables are deliberately generic. The LLM/stance layer can be added
-- later without changing the ingestion model.
CREATE TABLE IF NOT EXISTS questions (
    question_id INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id INTEGER NOT NULL REFERENCES threads(thread_id) ON DELETE CASCADE,
    question TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS stances (
    post_id INTEGER NOT NULL REFERENCES posts(post_id) ON DELETE CASCADE,
    question_id INTEGER NOT NULL REFERENCES questions(question_id) ON DELETE CASCADE,
    stance TEXT NOT NULL CHECK(stance IN (
        'strong_yes','probably_yes','uncertain','probably_no','strong_no','irrelevant','unclear'
    )),
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    model TEXT NOT NULL,
    rationale TEXT,
    PRIMARY KEY(post_id, question_id, model)
);
"""


class Database:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.conn = sqlite3.connect(path)
        self.conn.row_factory = sqlite3.Row
        self.conn.executescript(SCHEMA)

    def close(self) -> None:
        self.conn.close()

    def __enter__(self) -> "Database":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def store_page(self, parsed: ParsedPage) -> int:
        cur = self.conn.cursor()
        cur.execute(
            """INSERT INTO threads(thread_id, title, last_page_seen)
               VALUES (?, ?, ?)
               ON CONFLICT(thread_id) DO UPDATE SET
                   title=COALESCE(excluded.title, threads.title),
                   last_page_seen=MAX(threads.last_page_seen, excluded.last_page_seen),
                   updated_at=CURRENT_TIMESTAMP""",
            (parsed.thread_id, parsed.title, parsed.max_page),
        )

        stored = 0
        for post in parsed.posts:
            cur.execute("INSERT OR IGNORE INTO users(username) VALUES (?)", (post.author,))
            user_id = cur.execute("SELECT user_id FROM users WHERE username=?", (post.author,)).fetchone()[0]
            cur.execute(
                """INSERT INTO posts(post_id, thread_id, page, ordinal, user_id, posted_at, text, raw_text)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                   ON CONFLICT(post_id) DO UPDATE SET
                       page=excluded.page,
                       ordinal=excluded.ordinal,
                       user_id=excluded.user_id,
                       posted_at=excluded.posted_at,
                       text=excluded.text,
                       raw_text=excluded.raw_text""",
                (
                    post.post_id,
                    post.thread_id,
                    post.page,
                    post.ordinal,
                    user_id,
                    post.posted_at.isoformat() if post.posted_at else None,
                    post.text,
                    post.raw_text,
                ),
            )
            cur.execute("DELETE FROM quotes WHERE post_id=?", (post.post_id,))
            cur.executemany(
                "INSERT INTO quotes(post_id, quoted_post_id, quoted_author, quote_text) VALUES (?, ?, ?, ?)",
                [(post.post_id, q.post_id, q.author, q.text) for q in post.quotes],
            )
            cur.execute("DELETE FROM links WHERE post_id=?", (post.post_id,))
            cur.executemany(
                "INSERT OR IGNORE INTO links(post_id, url) VALUES (?, ?)",
                [(post.post_id, url) for url in post.links],
            )
            stored += 1

        self.conn.commit()
        return stored

    def thread_summary(self, thread_id: int) -> dict[str, object]:
        thread = self.conn.execute(
            "SELECT title, last_page_seen FROM threads WHERE thread_id=?", (thread_id,)
        ).fetchone()
        if not thread:
            raise KeyError(thread_id)

        totals = self.conn.execute(
            """SELECT COUNT(*) AS posts, COUNT(DISTINCT user_id) AS users,
                      MIN(posted_at) AS first_post, MAX(posted_at) AS last_post
               FROM posts WHERE thread_id=?""",
            (thread_id,),
        ).fetchone()

        top_users = self.conn.execute(
            """SELECT u.username, COUNT(*) AS posts
               FROM posts p JOIN users u ON u.user_id=p.user_id
               WHERE p.thread_id=?
               GROUP BY p.user_id
               ORDER BY posts DESC, u.username
               LIMIT 10""",
            (thread_id,),
        ).fetchall()

        top10_posts = sum(row["posts"] for row in top_users)
        post_count = int(totals["posts"] or 0)
        return {
            "thread_id": thread_id,
            "title": thread["title"],
            "last_page_seen": thread["last_page_seen"],
            "posts": post_count,
            "users": int(totals["users"] or 0),
            "first_post": totals["first_post"],
            "last_post": totals["last_post"],
            "top_users": [dict(row) for row in top_users],
            "top10_share": (top10_posts / post_count) if post_count else 0.0,
        }
