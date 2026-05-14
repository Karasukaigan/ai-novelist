import sqlite3
import threading
from pathlib import Path
from backend.settings.settings import settings

_local = threading.local()


def get_connection() -> sqlite3.Connection:
    """获取线程本地数据库连接，WAL 模式 + busy_timeout"""
    if not hasattr(_local, "conn") or _local.conn is None:
        db_path = settings.CONVERSATIONS_DB_PATH
        Path(db_path).parent.mkdir(parents=True, exist_ok=True)
        conn = sqlite3.connect(db_path, check_same_thread=False)
        conn.execute("PRAGMA journal_mode=WAL")
        conn.execute("PRAGMA busy_timeout=5000")
        conn.execute("PRAGMA foreign_keys=ON")
        conn.row_factory = sqlite3.Row
        _local.conn = conn
    return _local.conn


def close_connection():
    """关闭当前线程的数据库连接"""
    if hasattr(_local, "conn") and _local.conn is not None:
        _local.conn.close()
        _local.conn = None
