from __future__ import annotations

from collections import Counter
from dataclasses import dataclass
import hashlib
import re
import sqlite3

TOKEN_RE = re.compile(r"[\wåäöÅÄÖ]{3,}", re.UNICODE)
STOPWORDS = frozenset("alla att av blev bli blir de dem den det denna detta du där då efter eller en ett från för genom han har hela henne hennes honom i inte jag kan kommer med men mig min mot mycket när och om på som så till under upp vi vad var vara varför vem är också över the and for not this that with from have has was were you your they their".split())


@dataclass(frozen=True, slots=True)
class TopicCandidate:
    topic_id: int
    label: str
    post_count: int
    confidence: float
    method: str


def _terms(text: str) -> set[str]:
    return {token.lower() for token in TOKEN_RE.findall(text) if token.lower() not in STOPWORDS}


def discover_topics(conn: sqlite3.Connection, thread_id: int, *, limit: int = 10, min_post_count: int = 2) -> list[TopicCandidate]:
    """Create reproducible lexical topic candidates and post mappings."""
    if limit < 1 or min_post_count < 1:
        raise ValueError("limit och min_post_count måste vara minst 1")
    rows = conn.execute("SELECT post_id, text FROM posts WHERE thread_id=? ORDER BY post_id", (thread_id,)).fetchall()
    if not rows:
        if not conn.execute("SELECT 1 FROM threads WHERE thread_id=?", (thread_id,)).fetchone():
            raise KeyError(thread_id)
        conn.execute("DELETE FROM topics WHERE thread_id=?", (thread_id,))
        conn.commit()
        return []

    occurrences: Counter[str] = Counter()
    post_terms: dict[int, set[str]] = {}
    for row in rows:
        terms = _terms(row["text"] or "")
        post_terms[int(row["post_id"])] = terms
        occurrences.update(terms)
    candidates = [(term, count) for term, count in occurrences.items() if count >= min_post_count]
    candidates.sort(key=lambda item: (-item[1], item[0]))
    candidates = candidates[:limit]
    conn.execute("DELETE FROM topics WHERE thread_id=?", (thread_id,))
    if not candidates:
        conn.commit()
        return []

    input_hash = hashlib.sha256("|".join(f"{row['post_id']}:{row['text']}" for row in rows).encode()).hexdigest()
    max_count = candidates[0][1]
    result: list[TopicCandidate] = []
    for term, count in candidates:
        cursor = conn.execute("INSERT INTO topics(thread_id, label, method, confidence, input_hash) VALUES (?, ?, 'lexical-v1', ?, ?)", (thread_id, term, count / max_count, input_hash))
        topic_id = int(cursor.lastrowid)
        for post_id, terms in post_terms.items():
            if term in terms:
                conn.execute("INSERT INTO post_topics(post_id, topic_id, confidence, method) VALUES (?, ?, 1.0, 'lexical-v1')", (post_id, topic_id))
        result.append(TopicCandidate(topic_id, term, count, count / max_count, "lexical-v1"))
    conn.commit()
    return result
