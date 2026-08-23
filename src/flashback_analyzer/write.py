"""Write-side boundary, intentionally unimplemented until session UX is ready."""

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class ReplyDraft:
    thread_id: int
    text: str
    reply_to_post_id: int | None = None


class WriteClient(Protocol):
    def publish_reply(self, draft: ReplyDraft) -> int: ...


class ReadOnlyWriteClient:
    """Explicit guard so a future UI cannot silently post anonymously."""

    def publish_reply(self, draft: ReplyDraft) -> int:
        raise RuntimeError("Posting is not configured; provide an explicit authenticated write client.")
