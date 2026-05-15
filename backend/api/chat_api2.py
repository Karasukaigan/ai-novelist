from backend.settings.settings import settings
from backend.ai_agent.core.tool_load import import_tools
from backend.ai_agent.core.system_prompt_builder import SystemPromptBuilder
from backend.ai_agent.models.stream_interrupt_manager import stream_interrupt_manager
from backend.storage.schema import init_db
from backend.storage import service as storage
from litellm import acompletion

import json
import logging
import asyncio
import math
import uuid
import time
from pydantic import BaseModel, Field
from fastapi import APIRouter
from fastapi.responses import StreamingResponse


logger = logging.getLogger(__name__)

mode = settings.get_config("currentMode", default="管家agent")
selected_model = settings.get_config("selectedModel")
selected_provider = settings.get_config("selectedProvider")
temperature = settings.get_config("mode", mode, "temperature")
max_tokens = settings.get_config("mode", mode, "max_tokens")
# 确保数据库表已创建
init_db()

# 系统提示词构建器（每次调用 build_prompts 时创建新实例避免并发问题）
_system_prompt_builder = SystemPromptBuilder()


def _get_model_prefix(provider: str) -> str:
    if provider == "zhipuai":
        return "zai"
    elif provider == "ollama":
        return "ollama_chat"
    elif provider in ["deepseek", "dashscope", "openrouter", "gemini", "lm_studio", "moonshot"]:
        return provider
    else:
        return "openai"


def _count_tokens_approx(msg: dict) -> int:
    """估算单条消息的 token 数，对齐 langchain count_tokens_approximately"""
    chars = len(str(msg.get("content", "")))
    chars += len(msg.get("role", ""))
    if msg.get("role") == "assistant" and msg.get("tool_calls"):
        chars += len(repr(msg["tool_calls"]))
    if msg.get("role") == "tool":
        chars += len(msg.get("tool_call_id", ""))
    return math.ceil(chars / 4.0) + 3


def _trim_history(history: list[dict], max_tokens: int) -> list[dict]:
    """裁剪消息历史，对齐旧版 trim_messages(strategy=last, start_on=human, end_on=(human,tool))"""
    if not history:
        return []

    msgs = list(history)

    # Step 1: end_on — 从末尾截断到 user 或 tool
    while msgs and msgs[-1].get("role") not in ("user", "tool"):
        msgs.pop()

    if not msgs:
        return []

    # Step 2: 从尾到头累计 token，保留不超限的消息
    kept = []
    token_count = 0
    for msg in reversed(msgs):
        t = _count_tokens_approx(msg)
        if token_count + t > max_tokens:
            break
        kept.insert(0, msg)
        token_count += t

    # Step 3: start_on — 去掉开头不是 user 的消息
    while kept and kept[0].get("role") != "user":
        kept.pop(0)

    return kept


def _collect_summaries_and_filter(history: list[dict], summaries: list[dict]) -> tuple[str, list[dict]]:
    """收集摘要文本并过滤被替换的消息，返回 (summary_text, filtered_history)"""
    if not summaries:
        return "", list(history)

    # 收集所有被替换的消息 id
    skip_ids: set[str] = set()
    for s in summaries:
        # 从 history 中找到 replaces_from 到 replaces_to 之间的消息
        in_range = False
        for m in history:
            if m["id"] == s["replaces_from"]:
                in_range = True
            if in_range:
                skip_ids.add(m["id"])
            if m["id"] == s["replaces_to"]:
                break

    summary_text = "\n\n".join(s["content"] for s in summaries)
    filtered = [m for m in history if m["id"] not in skip_ids]
    return summary_text, filtered


def _tool_to_openai_schema(tool) -> dict:
    if hasattr(tool, 'args_schema') and tool.args_schema:
        if isinstance(tool.args_schema, dict):
            parameters = tool.args_schema
        else:
            parameters = tool.args_schema.model_json_schema()
    else:
        parameters = {"type": "object", "properties": {}}
    return {
        "type": "function",
        "function": {
            "name": tool.name,
            "description": tool.description,
            "parameters": parameters
        }
    }


class ChatMessageRequest(BaseModel):
    messages: list[dict] = Field(default_factory=list, description="OpenAI格式消息列表")


class RegenerateRequest(BaseModel):
    msg_id: str = Field(..., description="目标用户消息 id")
    edited_content: str | None = Field(default=None, description="编辑后的内容，纯重新生成为 null")


class SwitchBranchRequest(BaseModel):
    parent_msg_id: str = Field(..., description="分支点的父消息 id")
    target_msg_id: str = Field(..., description="要切换到的目标消息 id")


router = APIRouter(prefix="/api/chat2", tags=["Chat"])


class FunctionCallingRequest(BaseModel):
    """工具调用请求"""
    tool_call_id: str = Field(..., description="工具调用ID")
    approved: bool = Field(..., description="是否批准")
    user_extra: str = Field(default="", description="用户附加信息")
    user_diff: str | None = Field(default=None, description="用户对AI建议内容的修改diff")


def _build_state_update_data(thread_id: str) -> str:
    """构建包含完整树信息的 state_update JSON 字符串"""
    tree = storage.get_full_tree(thread_id)
    data = storage.get_data(thread_id)
    return json.dumps({
        "type": "state_update",
        "messages": tree["messages"],
        "active_leaf": tree["active_leaf"],
        "active_path": tree["active_path"],
        "branch_points": tree["branch_points"],
        "tool_requests": data.get("tool_requests", {}),
        "summaries": data.get("summaries", []),
    }, ensure_ascii=False) + "\n"


async def _build_messages_with_context(
    history: list[dict],
    mode: str,
    user_input: str = "",
    summaries: list[dict] | None = None,
) -> list[dict]:
    """将系统提示词和环境信息注入到消息列表中

    系统提示词 → 作为 system role 消息放在最前面
    环境信息（文件树/知识库/Skills/RAG等） → 作为 user role 消息放在最后面

    注入的消息不持久化到数据库，每次调用时动态构建。
    """
    # 收集摘要文本
    summary_text = ""
    if summaries:
        summary_text = "\n\n".join(s["content"] for s in summaries)

    system_prompt, context_message = await _system_prompt_builder.build_prompts(
        mode=mode,
        user_input=user_input,
        summary=summary_text,
    )

    result = []
    if system_prompt:
        result.append({"role": "system", "content": system_prompt})
    result.extend(history)
    if context_message:
        result.append({
            "role": "user",
            "content": f"【系统环境信息 - 此消息由系统自动生成，并非用户发送】\n\n{context_message}\n\n"
        })

    return result


# ==================== 流式 AI 响应（通用） ====================


async def _stream_ai_response(thread_id: str, parent_msg_id: str, history: list[dict]):
    """
    流式传输 AI 回复，结束后保存消息并更新 active_leaf。
    
    Args:
        thread_id: 会话 id
        parent_msg_id: AI 回复消息的 parent_id（通常是上一条用户消息 id）
        history: 发送给 AI 的活跃路径消息列表（已含注入上下文）
    """
    api_key = settings.get_provider_key(selected_provider)
    base_url = settings.get_config("provider", selected_provider, "url", default="")
    litellm_model = f"{_get_model_prefix(selected_provider)}/{selected_model}"

    tool_dict = await import_tools(mode=mode)
    tools = None
    if tool_dict:
        tools = [_tool_to_openai_schema(t) for t in tool_dict.values()]

    # 从消息列表中提取最后一条 user 消息作为 user_input
    user_input = ""
    for msg in reversed(history):
        if msg.get("role") == "user":
            user_input = msg.get("content", "")
            break

    # 收集摘要并过滤被替换的消息
    data = storage.get_data(thread_id)
    summaries = data.get("summaries", [])
    summary_text, filtered_history = _collect_summaries_and_filter(history, summaries)

    # 裁剪超限消息
    context_window = settings.get_config(
        "provider", selected_provider, "favoriteModels", "chat", selected_model
    ) or 4096
    trimmed_history = _trim_history(filtered_history, context_window - max_tokens)

    messages_with_context = await _build_messages_with_context(
        trimmed_history, mode, user_input, summaries
    )

    call_kwargs = {
        "model": litellm_model,
        "messages": messages_with_context,
        "temperature": temperature,
        "max_tokens": max_tokens,
        "timeout": 300,
        "stream": True,
        "stream_options": {"include_usage": True},
        "api_key": api_key,
        "base_url": base_url,
    }
    if tools:
        call_kwargs["tools"] = tools

    logger.info(f"AI 响应参数: model={litellm_model}")

    response_stream = await acompletion(**call_kwargs)

    full_content = ""
    full_reasoning = ""
    tool_calls_accumulated: dict[int, dict] = {}
    usage_metadata: dict | None = None

    async for chunk in response_stream:
        if stream_interrupt_manager.is_interrupted(thread_id):
            yield json.dumps({"interrupted": True}, ensure_ascii=False) + "\n"
            break

        # 捕获 usage（DeepSeek 等提供商将 usage 放在最后一个有 choices 的 chunk 中，而非空 choices chunk）
        if hasattr(chunk, 'usage') and chunk.usage:
            usage_metadata = {
                "input_tokens": chunk.usage.prompt_tokens,
                "output_tokens": chunk.usage.completion_tokens,
                "total_tokens": chunk.usage.total_tokens,
            }
            logger.info(f"[usage] 捕获到 usage: {usage_metadata}")
            yield json.dumps({"usage_metadata": usage_metadata}, ensure_ascii=False) + "\n"

        if not chunk.choices:
            continue

        delta = chunk.choices[0].delta
        chunk_data = {}

        if hasattr(delta, 'content') and delta.content:
            chunk_data["content"] = delta.content
            full_content += delta.content
            print(delta.content, end="|", flush=True)

        if hasattr(delta, 'reasoning_content') and delta.reasoning_content:
            chunk_data["reasoning_content"] = delta.reasoning_content
            full_reasoning += delta.reasoning_content

        if hasattr(delta, 'tool_calls') and delta.tool_calls:
            for tc in delta.tool_calls:
                index = tc.index if hasattr(tc, 'index') and tc.index is not None else 0
                if index not in tool_calls_accumulated:
                    tool_calls_accumulated[index] = {
                        "id": tc.id,
                        "type": "function",
                        "function": {
                            "name": tc.function.name if tc.function else None,
                            "arguments": tc.function.arguments if tc.function else ""
                        }
                    }
                else:
                    existing = tool_calls_accumulated[index]
                    if tc.id:
                        existing["id"] = tc.id
                    if tc.function:
                        if tc.function.name:
                            existing["function"]["name"] = tc.function.name
                        if tc.function.arguments:
                            existing["function"]["arguments"] += tc.function.arguments

            # 流式输出当前累积的 tool_calls，让前端能实时预览编辑效果
            chunk_data["tool_calls"] = [tc for tc in tool_calls_accumulated.values()]

        if chunk_data:
            yield json.dumps(chunk_data, ensure_ascii=False) + "\n"
            await asyncio.sleep(0)

    # 保存 assistant 消息到 data
    assistant_msg = {
        "id": f"msg-{uuid.uuid4()}",
        "role": "assistant",
        "content": full_content,
        "parent_id": parent_msg_id,
        "created_at": time.time(),
        "additional_kwargs": {},
    }
    if full_reasoning:
        assistant_msg["additional_kwargs"]["reasoning_content"] = full_reasoning
    if tool_calls_accumulated:
        assistant_msg["tool_calls"] = list(tool_calls_accumulated.values())
    if usage_metadata:
        assistant_msg["usage_metadata"] = usage_metadata

    data = storage.get_data(thread_id)
    data.setdefault("messages", []).append(assistant_msg)

    if tool_calls_accumulated:
        data["tool_requests"] = {}
        for tc in tool_calls_accumulated.values():
            data["tool_requests"][tc["id"]] = {
                "tool_name": tc["function"]["name"],
                "arguments": tc["function"].get("arguments", "{}"),
                "approved": None,
                "user_extra": None,
                "result": None,
            }

    # 更新 active_leaf 为新的 AI 消息
    data["active_leaf"] = assistant_msg["id"]
    storage.save_data(thread_id, data)

    # 发送统一 state_update
    yield _build_state_update_data(thread_id)


# ==================== 发送消息 ====================


@router.post("/message", summary="发送聊天消息（续接当前活跃路径）")
async def send_chat_message(request: ChatMessageRequest):
    thread_id = settings.get_config("thread_id")
    stream_interrupt_manager.create_task(thread_id)
    logger.info(f"为thread_id创建流式传输任务: {thread_id}")

    # 确保会话存在
    conv = storage.get_conversation(thread_id)
    if conv is None:
        title = request.messages[-1].get("content", "新对话") if request.messages else "新对话"
        storage.create_conversation(thread_id, title=title)

    # 获取当前活跃叶子作为 parent_id
    data = storage.get_data(thread_id)
    parent_id = data.get("active_leaf") or "__root__"

    # 保存用户消息（带 parent_id）
    if request.messages:
        user_msg = request.messages[-1]
        user_msg["parent_id"] = parent_id
        user_msg.setdefault("created_at", time.time())
        storage.append_message(thread_id, user_msg)
        # 更新 active_leaf 为用户消息
        data = storage.get_data(thread_id)
        data["active_leaf"] = user_msg["id"]
        storage.save_data(thread_id, data)

    async def generate():
        try:
            # 获取活跃路径作为 AI 上下文
            history = storage.get_active_path(thread_id)

            # 流式 AI 响应，parent_id 为新用户消息的 id
            async for chunk in _stream_ai_response(thread_id, user_msg["id"], history):
                yield chunk
        finally:
            stream_interrupt_manager.remove_task(thread_id)
            logger.info(f"清理流式传输任务: {thread_id}")

    return StreamingResponse(generate(), media_type="text/event-stream")


# ==================== 重新生成（创建分支） ====================


@router.post("/regenerate", summary="重新生成消息（创建新分支）")
async def regenerate_message(request: RegenerateRequest):
    thread_id = settings.get_config("thread_id")
    stream_interrupt_manager.create_task(thread_id)
    logger.info(f"重新生成: thread_id={thread_id}, msg_id={request.msg_id}")

    # 创建新分支用户消息，返回新消息 id
    new_user_msg_id = storage.regenerate(thread_id, request.msg_id, request.edited_content)

    async def generate():
        try:
            # 获取活跃路径作为 AI 上下文
            history = storage.get_active_path(thread_id)

            # 流式 AI 响应，parent_id 为新用户消息 id
            async for chunk in _stream_ai_response(thread_id, new_user_msg_id, history):
                yield chunk
        finally:
            stream_interrupt_manager.remove_task(thread_id)
            logger.info(f"清理流式传输任务: {thread_id}")

    return StreamingResponse(generate(), media_type="text/event-stream")


# ==================== 切换分支 ====================


@router.post("/switch-branch", summary="切换活跃分支")
async def switch_branch(request: SwitchBranchRequest):
    thread_id = settings.get_config("thread_id")
    tree = storage.switch_branch(thread_id, request.parent_msg_id, request.target_msg_id)
    return tree


# ==================== 工具函数（保持不变） ====================


async def _execute_tool(tool_dict: dict, tool_name: str, arguments: str) -> dict:
    """执行工具并返回结果"""
    tool = tool_dict.get(tool_name)
    if tool is None:
        return {"success": False, "detail": f"工具 '{tool_name}' 未找到"}

    try:
        args = json.loads(arguments) if arguments else {}
        result = await tool.ainvoke(args)
        return {"success": True, "detail": str(result)}
    except Exception as e:
        return {"success": False, "detail": f"工具执行失败: {str(e)}"}


# ==================== 工具调用处理 ====================


@router.post("/function_calling", summary="处理工具调用")
async def function_calling(request: FunctionCallingRequest):
    thread_id = settings.get_config("thread_id")
    logger.info(f"处理工具调用: tool_call_id={request.tool_call_id}, approved={request.approved}")

    # 1. 更新工具请求状态
    storage.update_tool_request(thread_id, request.tool_call_id, approved=request.approved)

    # 2. 执行工具
    result = None
    if request.approved:
        tr_info = storage.get_data(thread_id).get("tool_requests", {}).get(request.tool_call_id, {})
        tool_dict = await import_tools(mode=mode)
        result = await _execute_tool(tool_dict, tr_info.get("tool_name", ""), tr_info.get("arguments", "{}"))
    else:
        result = {"success": False, "detail": "用户取消了工具调用"}

    # 3. 保存结果到 data
    result_json = json.dumps(result, ensure_ascii=False)
    data = storage.get_data(thread_id)
    data.setdefault("tool_requests", {})
    if request.tool_call_id in data["tool_requests"]:
        data["tool_requests"][request.tool_call_id]["result"] = result_json

    # 获取当前 active_leaf 作为 tool 消息的 parent_id
    tool_parent_id = data.get("active_leaf")
    # 如果有用户diff，附加到工具结果中
    tool_content = result_json
    if request.user_diff:
        tool_content += f"\n\n[用户修改了文件内容]：\n{request.user_diff}"
    tool_msg = {
        "id": f"msg-{uuid.uuid4()}",
        "role": "tool",
        "tool_call_id": request.tool_call_id,
        "content": tool_content,
        "parent_id": tool_parent_id,
        "created_at": time.time(),
    }
    data.setdefault("messages", []).append(tool_msg)
    if request.user_extra:
        data.setdefault("tool_requests", {}).setdefault(request.tool_call_id, {})["user_extra"] = request.user_extra

    # 更新 active_leaf 为 tool 消息
    data["active_leaf"] = tool_msg["id"]
    storage.save_data(thread_id, data)

    async def generate():
        try:
            stream_interrupt_manager.create_task(thread_id)

            # 发送统一 state_update
            yield _build_state_update_data(thread_id)

            # 4. 检查是否还有待审批的工具
            pending = storage.get_pending_tool_requests(thread_id)

            if not pending:
                # 所有工具执行完毕，将 user_extra 追加到所有 tool 消息之后
                latest_data = storage.get_data(thread_id)
                for tid, info in latest_data.get("tool_requests", {}).items():
                    user_extra = info.get("user_extra")
                    if user_extra:
                        user_extra_msg = {
                            "id": f"msg-{uuid.uuid4()}",
                            "role": "user",
                            "content": user_extra,
                            "parent_id": latest_data.get("active_leaf"),
                            "created_at": time.time(),
                        }
                        latest_data["messages"].append(user_extra_msg)
                        latest_data["active_leaf"] = user_extra_msg["id"]
                storage.save_data(thread_id, latest_data)

                # 获取活跃路径作为 AI 上下文
                history = storage.get_active_path(thread_id)

                # 流式传输 AI 回复（parent_id 为最新 user_extra 或 tool 消息 id）
                ai_parent_id = latest_data.get("active_leaf")
                async for chunk in _stream_ai_response(thread_id, ai_parent_id, history):
                    yield chunk
        finally:
            stream_interrupt_manager.remove_task(thread_id)

    return StreamingResponse(generate(), media_type="text/event-stream")


# ==================== 上下文压缩 ====================


class SummarizeRequest(BaseModel):
    thread_id: str = Field(..., description="要压缩的会话 id")


@router.post("/summarize", summary="压缩上下文（生成摘要）")
async def summarize_context(request: SummarizeRequest):
    """手动触发上下文压缩，生成摘要存入 data.summaries"""
    thread_id = request.thread_id
    logger.info(f"压缩上下文: thread_id={thread_id}")

    data = storage.get_data(thread_id)
    history = storage.get_active_path(thread_id)
    summaries = data.get("summaries", [])

    if len(history) < 2:
        return {"success": False, "detail": "消息太少，无需压缩"}

    # 确定替换范围：压缩全部 active_path 消息
    replaces_from = history[0]["id"]
    replaces_to = history[-1]["id"]

    # 拼接总结 prompt（使用中文提示词，输出中文摘要）
    if summaries:
        existing = "\n\n".join(s["content"] for s in summaries)
        prompt = (
            f"以下是到目前为止的对话摘要：\n{existing}\n\n"
            "请根据以上对话和新的对话内容，用中文扩展或更新这个摘要。"
        )
    else:
        prompt = (
            "请用中文总结以上对话的主要内容，"
            "提取关键信息、重要决定和用户需求，形成一份简洁的对话摘要。"
        )

    # 构建总结消息（不含系统提示词和环境信息，纯对话历史）
    summarize_messages = list(history)
    summarize_messages.append({"role": "user", "content": prompt})

    # 调用总结模型
    api_key = settings.get_provider_key(selected_provider)
    base_url = settings.get_config("provider", selected_provider, "url", default="")
    litellm_model = f"{_get_model_prefix(selected_provider)}/{selected_model}"

    try:
        response = await acompletion(
            model=litellm_model,
            messages=summarize_messages,
            temperature=temperature,
            max_tokens=1024,
            timeout=120,
            api_key=api_key,
            base_url=base_url,
        )
        summary_content = response.choices[0].message.content or ""
    except Exception as e:
        logger.error(f"总结模型调用失败: {e}")
        return {"success": False, "detail": f"总结失败: {str(e)}"}

    # 存入 data.summaries
    data.setdefault("summaries", [])
    data["summaries"].append({
        "content": summary_content,
        "replaces_from": replaces_from,
        "replaces_to": replaces_to,
        "created_at": time.time(),
    })
    storage.save_data(thread_id, data)

    # 返回完整树信息（含 summaries）
    tree = storage.get_full_tree(thread_id)
    return {
        "messages": tree["messages"],
        "active_leaf": tree["active_leaf"],
        "active_path": tree["active_path"],
        "branch_points": tree["branch_points"],
        "summaries": data.get("summaries", []),
    }
