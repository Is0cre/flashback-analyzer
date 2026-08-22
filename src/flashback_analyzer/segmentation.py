from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta

import sqlite3


@dataclass(frozen=True, slots=True)
class SegmentBoundary:
    """A deterministic chronological slice of a thread."""

    number: int
    first_post_id: int
    last_post_id: int
    start_time: str | None
    end_time: str | None
    post_count: int


def _parse_time(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        return datetime.fromisoformat(value)
    except ValueError:
        return None


def build_segments(
    conn: sqlite3.Connection,
    thread_id: int,
    *,
    max_posts: int = 75,
    gap_hours: float = 24.0,
) -> list[SegmentBoundary]:
    """Rebuild and return chronological segment boundaries for a thread.

    A gap can create a boundary only after a segment has reached one third of
    the target size. This prevents a short pause from producing many tiny
    segments, while ensuring that very large segments never exceed max_posts.
    """
    if max_posts < 1:
        raise ValueError("max_posts måste vara minst 1")
    if gap_hours <= 0:
        raise ValueError("gap_hours måste vara större än 0")

    rows = conn.execute(
        """SELECT post_id, posted_at FROM posts WHERE thread_id=?
           ORDER BY COALESCE(posted_at, ''), COALESCE(ordinal, post_id), post_id""",
        (thread_id,),
    ).fetchall()
    if not rows:
        exists = conn.execute("SELECT 1 FROM threads WHERE thread_id=?", (thread_id,)).fetchone()
        if not exists:
            raise KeyError(thread_id)
        conn.execute("DELETE FROM segments WHERE thread_id=?", (thread_id,))
        conn.commit()
        return []

    minimum_for_gap = max(2, max_posts // 3)
    gap = timedelta(hours=gap_hours)
    groups: list[list[sqlite3.Row]] = []
    current: list[sqlite3.Row] = []
    previous_time: datetime | None = None
    for row in rows:
        current_time = _parse_time(row["posted_at"])
        split_for_size = len(current) >= max_posts
        split_for_gap = bool(current and len(current) >= minimum_for_gap and previous_time and current_time and current_time - previous_time >= gap)
        if current and (split_for_size or split_for_gap):
            groups.append(current)
            current = []
        current.append(row)
        previous_time = current_time or previous_time
    if current:
        groups.append(current)

    conn.execute("DELETE FROM segments WHERE thread_id=?", (thread_id,))
    boundaries: list[SegmentBoundary] = []
    for number, group in enumerate(groups, start=1):
        first = group[0]
        last = group[-1]
        boundary = SegmentBoundary(number, int(first["post_id"]), int(last["post_id"]), first["posted_at"], last["posted_at"], len(group))
        cursor = conn.execute(
            """INSERT INTO segments(thread_id, segment_number, first_post_id, last_post_id,
               start_time, end_time, post_count, summary, segmentation_version)
               VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?)""",
            (thread_id, boundary.number, boundary.first_post_id, boundary.last_post_id,
             boundary.start_time, boundary.end_time, boundary.post_count, "deterministic-v1"),
        )
        segment_id = cursor.lastrowid
        conn.executemany(
            "INSERT INTO segment_posts(segment_id, post_id) VALUES (?, ?)",
            [(segment_id, int(row["post_id"])) for row in group],
        )
        boundaries.append(boundary)
    conn.commit()
    return boundaries
