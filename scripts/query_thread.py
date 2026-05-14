"""
查询指定 thread 的数据库内容
用法: python scripts/query_thread.py <thread_id>
"""
import json
import sqlite3
import sys
from pathlib import Path

DATA_DIR = Path(__file__).parent.parent / "data"
DB_DIR = DATA_DIR / "db"


def query_conversations_db(thread_id: str):
    """查询 conversations.db"""
    db_path = DB_DIR / "conversations.db"
    if not db_path.exists():
        print(f"[!] 数据库不存在: {db_path}")
        return

    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row

    # 1. 查询会话
    cur = conn.execute(
        "SELECT * FROM conversations WHERE thread_id = ?", (thread_id,)
    )
    conv = cur.fetchone()
    if conv:
        print("=" * 60)
        print("📁 conversations.conversations")
        print("=" * 60)
        for k in conv.keys():
            print(f"  {k}: {conv[k]}")
    else:
        print(f"[!] conversations 表中未找到 thread_id={thread_id}")

    # 2. 查询消息
    cur = conn.execute(
        "SELECT * FROM messages WHERE thread_id = ? ORDER BY seq ASC", (thread_id,)
    )
    rows = cur.fetchall()
    if rows:
        print("\n" + "=" * 60)
        print(f"💬 conversations.messages (共 {len(rows)} 条)")
        print("=" * 60)
        for row in rows:
            d = dict(row)
            # 截断过长的 content
            content = d.get("content", "")
            if len(content) > 200:
                content = content[:200] + f"\n    ... (共 {len(d['content'])} 字符)"
            d["content"] = content
            print(f"\n  [id={d['id']}] role={d['role']} seq={d['seq']}")
            print(f"      content: {content}")
            print(f"      parent_id={d['parent_id']} branch_seq={d['branch_seq']}")
            print(f"      created_at={d['created_at']}")

    # 3. 查询 tool_requests
    cur = conn.execute(
        "SELECT * FROM tool_requests WHERE thread_id = ?", (thread_id,)
    )
    rows = cur.fetchall()
    if rows:
        print("\n" + "=" * 60)
        print(f"🔧 conversations.tool_requests (共 {len(rows)} 条)")
        print("=" * 60)
        for row in rows:
            d = dict(row)
            print(f"\n  tool_call_id={d['tool_call_id']}")
            print(f"  tool_name={d['tool_name']}")
            print(f"  arguments={d['arguments']}")
            print(f"  approved={d['approved']}  notified={d['notified']}")

    conn.close()


def query_checkpoints_db(thread_id: str):
    """查询 checkpoints.db"""
    db_path = DB_DIR / "checkpoints.db"
    if not db_path.exists():
        print(f"\n[!] 数据库不存在: {db_path}")
        return

    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row

    # 获取所有表名
    tables = conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table'"
    ).fetchall()
    table_names = [t["name"] for t in tables]

    for table in table_names:
        cur = conn.execute(f'SELECT * FROM "{table}"')
        rows = cur.fetchall()
        if not rows:
            continue

        # 检查是否有 thread_id 相关字段
        if rows and "thread_id" in rows[0].keys():
            filtered = [r for r in rows if r["thread_id"] == thread_id]
            if filtered:
                print("\n" + "=" * 60)
                print(f"🗄️  checkpoints.{table} (匹配 {len(filtered)} 条)")
                print("=" * 60)
                for row in filtered:
                    d = dict(row)
                    for k, v in d.items():
                        sv = str(v)
                        if len(sv) > 200:
                            sv = sv[:200] + f"... (共 {len(str(v))} 字符)"
                        print(f"  {k}: {sv}")
                    print()

    conn.close()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("用法: python scripts/query_thread.py <thread_id>")
        sys.exit(1)

    thread_id = sys.argv[1]
    print(f"🔍 查询 thread_id = {thread_id}\n")

    query_conversations_db(thread_id)
    query_checkpoints_db(thread_id)
