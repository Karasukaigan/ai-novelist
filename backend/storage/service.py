import json
import time
import uuid
import logging
from typing import Optional

from backend.storage.connection import get_connection
from backend.storage.models import Conversation

logger = logging.getLogger(__name__)


# ==================== Conversation CRUD（保持不变） ====================


def create_conversation(thread_id: str, title: str = "新对话") -> Conversation:
    """创建新会话，初始 data 为空消息列表和空工具请求字典"""
    conn = get_connection()
    now = time.time()
    data = json.dumps({"messages": [], "tool_requests": {}, "active_leaf": None}, ensure_ascii=False)
    conn.execute(
        "INSERT OR IGNORE INTO conversations (thread_id, title, created_at, updated_at, data) VALUES (?, ?, ?, ?, ?)",
        (thread_id, title, now, now, data),
    )
    conn.commit()
    return Conversation(thread_id=thread_id, title=title, created_at=now, updated_at=now)


def get_conversation(thread_id: str) -> Optional[Conversation]:
    """获取会话（含 message_count）"""
    conn = get_connection()
    row = conn.execute(
        "SELECT thread_id, title, created_at, updated_at, data FROM conversations WHERE thread_id = ?",
        (thread_id,),
    ).fetchone()
    if row is None:
        return None
    data = json.loads(row["data"]) if row["data"] else {}
    msg_count = len(data.get("messages", []))
    return Conversation(
        thread_id=row["thread_id"],
        title=row["title"],
        created_at=row["created_at"],
        updated_at=row["updated_at"],
        msg_count=msg_count,
    )


def list_conversations() -> list[Conversation]:
    """获取所有会话列表"""
    conn = get_connection()
    rows = conn.execute(
        "SELECT thread_id, title, created_at, updated_at, data FROM conversations ORDER BY updated_at DESC"
    ).fetchall()
    result = []
    for r in rows:
        data = json.loads(r["data"]) if r["data"] else {}
        msg_count = len(data.get("messages", []))
        result.append(Conversation(
            thread_id=r["thread_id"],
            title=r["title"],
            created_at=r["created_at"],
            updated_at=r["updated_at"],
            msg_count=msg_count,
        ))
    return result


def delete_conversation(thread_id: str) -> bool:
    """删除会话"""
    conn = get_connection()
    conn.execute("DELETE FROM conversations WHERE thread_id = ?", (thread_id,))
    conn.commit()
    return True


# ==================== Data JSON 基础操作（保持不变） ====================


def _touch(thread_id: str):
    """更新 updated_at 时间戳"""
    conn = get_connection()
    conn.execute(
        "UPDATE conversations SET updated_at = ? WHERE thread_id = ?",
        (time.time(), thread_id),
    )
    conn.commit()


def get_data(thread_id: str) -> dict:
    """获取会话的完整 data JSON（已解析为 dict）"""
    conn = get_connection()
    row = conn.execute(
        "SELECT data FROM conversations WHERE thread_id = ?",
        (thread_id,),
    ).fetchone()
    if row is None:
        return {"messages": [], "tool_requests": {}, "active_leaf": None}
    return json.loads(row["data"]) if row["data"] else {"messages": [], "tool_requests": {}, "active_leaf": None}


def save_data(thread_id: str, data: dict):
    """原子替换整个 data JSON"""
    conn = get_connection()
    now = time.time()
    conn.execute(
        "UPDATE conversations SET data = ?, updated_at = ? WHERE thread_id = ?",
        (json.dumps(data, ensure_ascii=False), now, thread_id),
    )
    conn.commit()


def append_message(thread_id: str, message: dict):
    """向 data.messages 追加一条消息"""
    data = get_data(thread_id)
    data.setdefault("messages", []).append(message)
    save_data(thread_id, data)


# ==================== Tool Request 操作（保持不变） ====================


def add_tool_request(thread_id: str, tool_call_id: str, tool_name: str, arguments: str):
    data = get_data(thread_id)
    data.setdefault("tool_requests", {})[tool_call_id] = {
        "tool_name": tool_name,
        "arguments": arguments,
        "approved": None,
        "user_extra": None,
        "result": None,
    }
    save_data(thread_id, data)


def update_tool_request(thread_id: str, tool_call_id: str, **updates):
    data = get_data(thread_id)
    trs = data.setdefault("tool_requests", {})
    if tool_call_id in trs:
        trs[tool_call_id].update(updates)
        save_data(thread_id, data)


def get_pending_tool_requests(thread_id: str) -> list[tuple[str, dict]]:
    data = get_data(thread_id)
    trs = data.get("tool_requests", {})
    return [(tid, info) for tid, info in trs.items() if info.get("approved") is None]


# ==================== 分支树核心算法 ====================


def _build_msg_map(messages: list) -> dict:
    """构建 id → message 的映射"""
    return {m["id"]: m for m in messages}


def _get_active_path(messages: list, active_leaf: str | None) -> list[dict]:
    """
    从 active_leaf 回溯到根，返回有序消息列表。
    纯函数，供内部和外部调用。
    """
    if not active_leaf or not messages:
        return []

    msg_map = _build_msg_map(messages)
    path = []
    current_id = active_leaf
    while current_id:
        msg = msg_map.get(current_id)
        if not msg:
            break
        path.insert(0, msg)
        current_id = msg.get("parent_id")
    return path


def get_active_path(thread_id: str) -> list[dict]:
    """从 active_leaf 回溯到根，返回有序消息列表（发给 AI 的上下文）"""
    data = get_data(thread_id)
    return _get_active_path(data.get("messages", []), data.get("active_leaf"))


def _compute_branch_points(messages: list, active_leaf: str | None) -> list[dict]:
    """
    计算所有分支点信息（给前端展示用）。
    返回值示例：
    [{
        "at_msg_id": "m2",
        "variants": ["m3", "m5", "m7"],
        "active": "m7",
        "current_index": 2,
        "total": 3
    }]
    """
    import logging
    logger = logging.getLogger(__name__)

    if not messages:
        return []

    # parent_id → [child_id, ...]
    parent_children: dict[str, list[str]] = {}
    created_at: dict[str, float] = {}

    for m in messages:
        pid = m.get("parent_id")
        if pid:
            parent_children.setdefault(pid, []).append(m["id"])
        created_at[m["id"]] = m.get("created_at", 0)

    # 按 created_at 排序每个父级下的子消息
    for pid in parent_children:
        parent_children[pid].sort(key=lambda cid: created_at.get(cid, 0))

    # 找到活跃路径上的消息 id 集合
    active_path_ids = set()
    if active_leaf:
        msg_map = _build_msg_map(messages)
        current_id = active_leaf
        while current_id:
            active_path_ids.add(current_id)
            msg = msg_map.get(current_id)
            if not msg:
                break
            current_id = msg.get("parent_id")

    branch_points = []
    for pid, children in parent_children.items():
        if len(children) > 1:
            active = next((c for c in children if c in active_path_ids), children[-1])
            current_idx = children.index(active)
            branch_points.append({
                "at_msg_id": pid,
                "variants": children,
                "active": active,
                "current_index": current_idx,
                "total": len(children),
            })

    return branch_points


def _get_branch_leaf(messages: list, start_msg_id: str) -> str:
    """
    从 start_msg_id 出发，沿路径走到末端叶子。
    如果分叉，取 created_at 最新的 child。
    """
    msg_map = _build_msg_map(messages)
    current = start_msg_id
    while current:
        children = [m for m in messages if m.get("parent_id") == current]
        if not children:
            return current
        # 取 created_at 最新的 child
        children.sort(key=lambda m: m.get("created_at", 0), reverse=True)
        current = children[0]["id"]
    return start_msg_id


def regenerate(thread_id: str, msg_id: str, edited_content: str | None = None) -> str:
    """
    从指定消息创建新分支。
    返回新创建的用户消息 id（供后续 AI 回复流式写入时引用 parent_id）。
    """
    data = get_data(thread_id)
    messages = data.get("messages", [])
    msg_map = _build_msg_map(messages)

    target_msg = msg_map.get(msg_id)
    if not target_msg:
        raise ValueError(f"消息不存在: {msg_id}")

    # 如果目标消息的 parent_id 为 None（根级消息），使用 "__root__" 替代
    target_parent = target_msg.get("parent_id")
    new_parent_id = target_parent if target_parent else "__root__"
    new_user_msg = {
        "id": f"msg-{uuid.uuid4()}",
        "role": "user",
        "content": edited_content if edited_content is not None else target_msg["content"],
        "parent_id": new_parent_id,
        "created_at": time.time(),
    }

    messages.append(new_user_msg)
    data["active_leaf"] = new_user_msg["id"]
    save_data(thread_id, data)
    return new_user_msg["id"]


def switch_branch(thread_id: str, parent_msg_id: str, target_msg_id: str) -> dict:
    """
    切换活跃分支。
    验证 target_msg_id 的 parent_id == parent_msg_id，然后找到分支末端叶子设为 active_leaf。
    返回完整树信息。
    """
    data = get_data(thread_id)
    messages = data.get("messages", [])
    msg_map = _build_msg_map(messages)

    target_msg = msg_map.get(target_msg_id)
    if not target_msg:
        raise ValueError(f"目标消息不存在: {target_msg_id}")
    if target_msg.get("parent_id") != parent_msg_id:
        raise ValueError(f"目标消息 {target_msg_id} 的 parent_id 不匹配 {parent_msg_id}")

    leaf = _get_branch_leaf(messages, target_msg_id)
    data["active_leaf"] = leaf
    save_data(thread_id, data)

    return get_full_tree(thread_id)


def get_full_tree(thread_id: str) -> dict:
    """
    返回完整树信息给前端。
    { messages, active_leaf, branch_points, active_path, tool_requests }
    active_path 是后端计算好的当前活跃路径消息列表，前端直接用于渲染。
    """
    data = get_data(thread_id)
    messages = data.get("messages", [])
    active_leaf = data.get("active_leaf")
    active_path = _get_active_path(messages, active_leaf)
    branch_points = _compute_branch_points(messages, active_leaf)
    return {
        "messages": messages,
        "active_leaf": active_leaf,
        "active_path": active_path,
        "branch_points": branch_points,
        "tool_requests": data.get("tool_requests", {}),
    }


def delete_message_cascade(thread_id: str, msg_id: str) -> dict:
    """
    级联删除消息及其所有后代（所有分支），确保不留孤儿数据。
    如果 active_leaf 被删除，回退到最近的存活祖先。
    返回删除后的完整树信息。
    """
    data = get_data(thread_id)
    messages = data.get("messages", [])
    msg_map = _build_msg_map(messages)

    # 1. 递归收集所有需要删除的 id
    to_delete = set()

    def _collect_descendants(node_id: str):
        to_delete.add(node_id)
        for m in messages:
            if m.get("parent_id") == node_id:
                _collect_descendants(m["id"])

    _collect_descendants(msg_id)

    # 2. 过滤掉被删除的消息
    data["messages"] = [m for m in messages if m["id"] not in to_delete]

    # 3. 如果 active_leaf 被删了，回退到最近的存活祖先
    if data.get("active_leaf") in to_delete:
        data["active_leaf"] = _find_safe_leaf(data["messages"], msg_map.get(msg_id, {}).get("parent_id"))

    save_data(thread_id, data)
    return get_full_tree(thread_id)


def _find_safe_leaf(messages: list, from_parent_id: str | None) -> str | None:
    """
    从 from_parent_id 开始找最近的存活叶子。
    如果 from_parent_id 对应的消息也不存在了，继续往上回溯。
    兜底：返回 messages 中第一条消息的 id。
    """
    if not messages:
        return None

    msg_map = _build_msg_map(messages)
    current_id = from_parent_id

    # 如果当前 parent 也被删了，向上回溯
    while current_id and current_id not in msg_map:
        parent = msg_map.get(current_id)
        if parent:
            current_id = parent.get("parent_id")
        else:
            # 从原始 messages 中找
            for m in messages:
                if m["id"] == current_id:
                    current_id = m.get("parent_id")
                    break
            else:
                current_id = None

    if current_id and current_id in msg_map:
        return _get_branch_leaf(messages, current_id)

    # 兜底：返回第一条消息
    return messages[0]["id"]


