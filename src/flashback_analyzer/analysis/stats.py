from __future__ import annotations

import sqlite3


def participation_concentration(conn: sqlite3.Connection, thread_id: int) -> dict[str, float]:
    rows = conn.execute(
        """SELECT COUNT(*) AS n
           FROM posts
           WHERE thread_id=?
           GROUP BY user_id
           ORDER BY n DESC""",
        (thread_id,),
    ).fetchall()
    counts = [int(r[0]) for r in rows]
    total = sum(counts)
    if not total:
        return {"top_1_user_share": 0.0, "top_10_users_share": 0.0, "hhi": 0.0, "gini": 0.0}

    shares = [c / total for c in counts]
    hhi = sum(s * s for s in shares)

    # Gini over post counts per participating account.
    ordered = sorted(counts)
    n = len(ordered)
    weighted = sum((i + 1) * value for i, value in enumerate(ordered))
    gini = (2 * weighted) / (n * total) - (n + 1) / n if n else 0.0

    return {
        "top_1_user_share": counts[0] / total,
        "top_10_users_share": sum(counts[:10]) / total,
        "hhi": hhi,
        "gini": gini,
    }
