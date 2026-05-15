import json
import logging
from pydantic import BaseModel, Field
from fastapi import APIRouter
from backend.settings.settings import settings
# 小写的时全局实例，导入实例能确保唯一，导入类名则每个文件都不同
from backend.ai_agent.models.stream_interrupt_manager import stream_interrupt_manager

logger = logging.getLogger(__name__)

# ==================== 请求模型 ====================

class InterruptStreamRequest(BaseModel):
    """中断流式传输请求"""
    thread_id: str = Field(..., description="线程ID")

class SelectedModelRequest(BaseModel):
    """设置选中模型请求"""
    selectedModel: str = Field(..., description="选中的模型ID")
    selectedProvider: str = Field(..., description="选中的提供商ID")

class AutoApproveSettingsRequest(BaseModel):
    """设置自动批准配置请求"""
    enabled: bool = Field(..., description="是否启用自动批准")


router = APIRouter(prefix="/api/chat", tags=["Chat"])


# ==================== 流式中断控制 ====================

@router.post("/interrupt-stream", summary="中断流式传输")
async def interrupt_stream(request: InterruptStreamRequest):
    """
    中断正在进行的流式传输

    - **thread_id**: 线程ID
    """
    thread_id = request.thread_id
    success = stream_interrupt_manager.interrupt_task(thread_id)

    if success:
        logger.info(f"成功中断流式传输: {thread_id}")
        return {"success": True, "message": "流式传输已中断"}
    else:
        logger.warning(f"中断流式传输失败，任务不存在: {thread_id}")
        return {"success": False, "message": "任务不存在或已结束"}


# ==================== 模型选择 ====================

@router.get("/selected-model", summary="获取选中的模型")
async def get_selected_model():
    """
    获取当前选中的模型和提供商

    返回:
    - selectedModel: 选中的模型ID
    - selectedProvider: 选中的提供商ID
    """
    selected_model = settings.get_config("selectedModel", default="")
    selected_provider = settings.get_config("selectedProvider", default="")

    logger.info(f"获取选中的模型: {selected_model}, 提供商: {selected_provider}")

    return {
        "selectedModel": selected_model,
        "selectedProvider": selected_provider
    }


@router.post("/selected-model", summary="设置选中的模型")
async def set_selected_model(request: SelectedModelRequest):
    """
    设置选中的模型和提供商

    - **selectedModel**: 选中的模型ID
    - **selectedProvider**: 选中的提供商ID

    返回:
    - success: 是否成功
    """
    settings.update_config(request.selectedModel, "selectedModel")
    settings.update_config(request.selectedProvider, "selectedProvider")

    logger.info(f"设置选中的模型: {request.selectedModel}, 提供商: {request.selectedProvider}")

    return {
        "success": True,
        "selectedModel": request.selectedModel,
        "selectedProvider": request.selectedProvider
    }


# ==================== 自动审批 ====================

@router.get("/auto-approve", summary="获取自动批准配置")
async def get_auto_approve():
    """
    获取自动批准配置

    返回:
    - enabled: 是否启用自动批准
    """
    enabled = settings.get_config("autoApproveSettings", default=False)

    logger.info(f"获取自动批准配置: enabled={enabled}")

    return {
        "enabled": enabled
    }


@router.post("/auto-approve", summary="设置自动批准配置")
async def set_auto_approve(request: AutoApproveSettingsRequest):
    """
    设置自动批准配置

    - **enabled**: 是否启用自动批准

    返回:
    - success: 是否成功
    - enabled: 设置后的启用状态
    """
    settings.update_config(request.enabled, "autoApproveSettings")

    logger.info(f"设置自动批准配置: enabled={request.enabled}")

    return {
        "success": True,
        "enabled": request.enabled
    }
