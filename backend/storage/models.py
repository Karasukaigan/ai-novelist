from dataclasses import dataclass


@dataclass
class Conversation:
    thread_id: str
    title: str
    created_at: float
    updated_at: float
    msg_count: int = 0
