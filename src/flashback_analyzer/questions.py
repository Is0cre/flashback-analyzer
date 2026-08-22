from __future__ import annotations

from collections import Counter, defaultdict
from dataclasses import dataclass
import hashlib
import re
import sqlite3


SENTENCE_RE = re.compile(r"[^\n.!?]{8,}\?")


@dataclass(frozen=True, slots=True)
class QuestionCandidate:
    question_id: int
    question: str
    post_count: int
    confidence: float
    method: str


def _question_key(value: str) -> str:
    return re.sub(r"\s+", " ", value.strip()).casefold()


def discover_questions(conn: sqlite3.Connection, thread_id: int, *, limit: int = 20, min_post_count: int = 1) -> list[QuestionCandidate]:
    """Store recurring explicit questions found in original post text."""
    if limit < 1 or min_post_count < 1:
        raise ValueError("limit och min_post_count måste vara minst 1")
    rows = conn.execute("SELECT post_id, text FROM posts WHERE thread_id=? ORDER BY post_id", (thread_id,)).fetchall()
    if not rows:
        if not conn.execute("SELECT 1 FROM threads WHERE thread_id=?", (thread_id,)).fetchone():
            raise KeyError(thread_id)
        conn.execute("DELETE FROM questions WHERE thread_id=?", (thread_id,))
        conn.commit()
        return []

    counts: Counter[str] = Counter()
    labels: dict[str, str] = {}
    evidence: defaultdict[str, set[int]] = defaultdict(set)
    for row in rows:
        seen: set[str] = set()
        for match in SENTENCE_RE.finditer(row["text"] or ""):
            question = re.sub(r"\s+", " ", match.group(0).strip())
            key = _question_key(question)
            if len(question) < 12 or key in seen:
                continue
            seen.add(key)
            counts[key] += 1
            labels.setdefault(key, question)
            evidence[key].add(int(row["post_id"]))
    candidates = [(key, count) for key, count in counts.items() if count >= min_post_count]
    candidates.sort(key=lambda item: (-item[1], item[0]))
    candidates = candidates[:limit]
    conn.execute("DELETE FROM questions WHERE thread_id=?", (thread_id,))
    if not candidates:
        conn.commit()
        return []

    input_hash = hashlib.sha256("|".join(f"{row['post_id']}:{row['text']}" for row in rows).encode()).hexdigest()
    max_count = candidates[0][1]
    result: list[QuestionCandidate] = []
    for key, count in candidates:
        question = labels[key]
        cursor = conn.execute("""INSERT INTO questions(thread_id, question, active, method, confidence, input_hash)
            VALUES (?, ?, 1, 'explicit-v1', ?, ?)""", (thread_id, question, count / max_count, input_hash))
        question_id = int(cursor.lastrowid)
        for post_id in evidence[key]:
            conn.execute("INSERT INTO post_questions(post_id, question_id, confidence, method) VALUES (?, ?, 1.0, 'explicit-v1')", (post_id, question_id))
        result.append(QuestionCandidate(question_id, question, count, count / max_count, "explicit-v1"))
    conn.commit()
    return result
