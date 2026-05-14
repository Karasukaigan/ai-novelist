from backend.storage.connection import get_connection

SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS conversations (
    thread_id TEXT PRIMARY KEY,
    title     TEXT NOT NULL DEFAULT '新对话',
    created_at REAL NOT NULL,
    updated_at REAL NOT NULL,
    data      TEXT NOT NULL DEFAULT '{}'
);
"""

def init_db():
    """初始化数据库表结构"""
    conn = get_connection()
    conn.executescript(SCHEMA_SQL)
    conn.commit()
