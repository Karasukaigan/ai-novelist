"""基于Git的检查点API，用于管理文件归档。

通过HTTP转发请求到Go启动器的Git服务（默认端口18080）。
"""

import logging
import os
from typing import Optional
import httpx
from pydantic import BaseModel, Field
from fastapi import APIRouter, HTTPException

from backend.settings.settings import settings

logger = logging.getLogger(__name__)

# 创建路由
checkpoint_router = APIRouter(prefix="/api/checkpoints", tags=["checkpoints"])

# Git服务地址，从环境变量读取，默认 localhost:18080
GIT_SERVICE_URL = os.environ.get("GIT_SERVICE_URL", "http://localhost:18080")


async def _git_get(path: str):
    """发送GET请求到Go Git服务。"""
    async with httpx.AsyncClient() as client:
        url = f"{GIT_SERVICE_URL}{path}"
        try:
            resp = await client.get(url, timeout=30.0)
            resp.raise_for_status()
            return resp.json()
        except httpx.ConnectError as e:
            logger.error(f"无法连接到Git服务 {url}: {e}")
            raise HTTPException(status_code=503, detail="Git服务未启动或无法连接")
        except httpx.HTTPStatusError as e:
            logger.error(f"Git服务返回错误 {e.response.status_code}: {e.response.text}")
            raise HTTPException(status_code=e.response.status_code, detail=e.response.text)


async def _git_post(path: str, json_data: dict):
    """发送POST请求到Go Git服务。"""
    async with httpx.AsyncClient() as client:
        url = f"{GIT_SERVICE_URL}{path}"
        try:
            resp = await client.post(url, json=json_data, timeout=30.0)
            resp.raise_for_status()
            return resp.json()
        except httpx.ConnectError as e:
            logger.error(f"无法连接到Git服务 {url}: {e}")
            raise HTTPException(status_code=503, detail="Git服务未启动或无法连接")
        except httpx.HTTPStatusError as e:
            logger.error(f"Git服务返回错误 {e.response.status_code}: {e.response.text}")
            raise HTTPException(status_code=e.response.status_code, detail=e.response.text)


class SaveCheckpointRequest(BaseModel):
    """保存检查点请求。"""
    message: Optional[str] = Field(default=None, description="检查点消息。如果未提供，将使用自动生成的时间戳。")


class RestoreCheckpointRequest(BaseModel):
    """恢复检查点请求。"""
    commit_hash: str = Field(..., description="要恢复的提交哈希")


# API 端点
@checkpoint_router.get("/status")
async def get_status():
    """
    获取当前Git状态。
    """
    try:
        return await _git_get("/api/checkpoints/status")
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"获取状态失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@checkpoint_router.get("/list")
async def list_checkpoints():
    """
    列出所有检查点。
    """
    try:
        return await _git_get("/api/checkpoints/list")
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"列出检查点失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@checkpoint_router.post("/save")
async def save_checkpoint(request: SaveCheckpointRequest):
    """
    将当前状态保存为检查点。
    """
    try:
        return await _git_post("/api/checkpoints/save", {"message": request.message or ""})
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"保存检查点失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@checkpoint_router.post("/restore")
async def restore_checkpoint(request: RestoreCheckpointRequest):
    """
    将工作区恢复到指定检查点。
    """
    try:
        return await _git_post("/api/checkpoints/restore", {"commit_hash": request.commit_hash})
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"恢复检查点失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@checkpoint_router.get("/diff/{commit_hash}")
async def get_checkpoint_diff(commit_hash: str):
    """
    获取检查点与上一个检查点之间的差异。
    """
    try:
        return await _git_get(f"/api/checkpoints/diff/{commit_hash}")
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"获取差异失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@checkpoint_router.get("/working-diff/{file_path:path}")
async def get_working_diff(file_path: str):
    """
    获取当前工作区中指定文件与最新提交之间的差异。
    """
    try:
        # URL编码文件路径
        encoded_path = file_path.replace("/", "%2F").replace("\\", "%2F")
        return await _git_get(f"/api/checkpoints/working-diff/{encoded_path}")
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"获取工作区差异失败: {e}")
        raise HTTPException(status_code=500, detail=str(e))
