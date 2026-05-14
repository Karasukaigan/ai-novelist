import logging

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from backend.storage import service as storage

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/api/history", tags=["History"])


# ==================== 请求模型 ====================

class DeleteByContentIdRequest(BaseModel):
    thread_id: str = Field(default="default", description="会话ID")
    content_id: str = Field(..., description="消息的 content ID（前端生成的 id 字段）")


# ==================== 会话管理 ====================

@router.get("/sessions", summary="获取所有会话列表")
async def get_all_sessions():
    conversations = storage.list_conversations()
    return {
        "sessions": [
            {
                "session_id": c.thread_id,
                "message_count": c.msg_count,
                "created_at": c.created_at,
                "last_accessed": c.updated_at,
                "preview": c.title,
            }
            for c in conversations
        ]
    }


@router.delete("/sessions/{session_id}", summary="删除指定会话")
async def delete_session(session_id: str):
    conv = storage.get_conversation(session_id)
    if conv is None:
        raise HTTPException(status_code=404, detail="会话不存在")
    storage.delete_conversation(session_id)
    return {"message": f"会话 {session_id} 已删除"}


# ==================== 消息管理 ====================

@router.get("/messages/{thread_id}", summary="获取指定会话的完整树信息")
async def get_messages(thread_id: str):
    """返回完整消息树（含 active_leaf 和 branch_points）"""
    return storage.get_full_tree(thread_id)


@router.post("/messages/delete-by-id", summary="删除消息（级联删除所有分支）")
async def delete_message_by_content_id(request: DeleteByContentIdRequest):
    """级联删除消息及其所有后代，返回删除后的完整树"""
    tree = storage.delete_message_cascade(request.thread_id, request.content_id)
    return tree
