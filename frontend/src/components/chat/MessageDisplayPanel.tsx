import { useRef, useState, useEffect } from 'react';
import { useDispatch, useSelector } from 'react-redux';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faAngleRight, faAngleUp, faTrash, faRotateRight, faEdit, faCopy, faCheck, faChevronLeft, faChevronRight } from '@fortawesome/free-solid-svg-icons';
import type { RootState } from '../../types';
import type { Message, StreamChunk, ToolCall, BranchPoint } from '../../types/langgraph';
import { setAvailableTools } from '../../store/mode';
import { createAiMessage, updateAiMessage, updateMessages, setIsStreaming, setMessagesTree, setCurrentToolRequest } from '../../store/chat';
import httpClient from '../../utils/httpClient';
import UnifiedModal from '../others/UnifiedModal';
import { tryCompleteJSON } from '../../utils/jsonUtils';
import { useFileToolHandler } from '../../utils/fileToolHandler';

const MessageDisplayPanel = () => {
  const dispatch = useDispatch();
  const { processFileToolCalls } = useFileToolHandler();
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const [expandedToolResults, setExpandedToolResults] = useState<Set<string>>(new Set());
  const [expandedReasonings, setExpandedReasonings] = useState<Set<string>>(new Set());
  const emptyMessages: Message[] = [];
  const emptyInterrupts: any[] = [];
  // 消息模态框状态
  const [modal, setModal] = useState<{ show: boolean; message: string; onConfirm: (() => void) | null; onCancel: (() => void) | null }>({
    show: false,
    message: '',
    onConfirm: null,
    onCancel: null
  });
  
  // 内联编辑状态
  const [editingMessageId, setEditingMessageId] = useState<string | null>(null);
  const [editingContent, setEditingContent] = useState<string>('');
  const [editingMessageType, setEditingMessageType] = useState<'human' | 'ai'>('human');
  
  // 从Redux获取可用工具信息
  const availableTools = useSelector((state: RootState) => state.modeSlice.availableTools);
  
  // 从Redux获取消息列表和分支树信息
  const messages = useSelector((state: RootState) => state.chatSlice.state?.values?.messages || emptyMessages);
  const chatState = useSelector((state: RootState) => state.chatSlice.state);
  const branchPoints = useSelector((state: RootState) => state.chatSlice.branchPoints || []);
  const allMessages = useSelector((state: RootState) => state.chatSlice.allMessages || []);
  // 从Redux获取thread_id和mode
  const threadId = useSelector((state: RootState) => state.chatSlice.selectedThreadId) || 'default';
  const selectedModeId = useSelector((state: RootState) => state.modeSlice.selectedModeId) || '管家agent';
  
  // 从Redux获取中断状态
  const interrupts = useSelector((state: RootState) => state.chatSlice.state?.interrupts || emptyInterrupts);
  const isInterrupted = interrupts.length > 0;

  // 加载可用工具数据
  useEffect(() => {
    const loadTools = async () => {
      try {
        const toolsResult = await httpClient.get('/api/mode/tool/available-tools');
        if (toolsResult) {
          dispatch(setAvailableTools(toolsResult));
        }
      } catch (error) {
        setModal({ show: true, message: (error as Error).toString(), onConfirm: null, onCancel: null });
      }
    };
    loadTools();
  }, []);

  // 自动滚动到最新消息
  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  // 切换工具展开/折叠状态
  const toggleToolExpand = (msgId: string, toolIndex: number) => {
    const key = `${msgId}-${toolIndex}`;
    setExpandedTools(prev => {
      const newSet = new Set(prev);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      return newSet;
    });
  };

  // 切换工具结果展开/折叠状态
  const toggleToolResultExpand = (msgId: string) => {
    setExpandedToolResults(prev => {
      const newSet = new Set(prev);
      if (newSet.has(msgId)) {
        newSet.delete(msgId);
      } else {
        newSet.add(msgId);
      }
      return newSet;
    });
  };

  // 切换思维链展开/折叠状态
  const toggleReasoningExpand = (msgId: string) => {
    setExpandedReasonings(prev => {
      const newSet = new Set(prev);
      if (newSet.has(msgId)) {
        newSet.delete(msgId);
      } else {
        newSet.add(msgId);
      }
      return newSet;
    });
  };

  // 复制消息内容到剪贴板
  const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null);
  const copyMessage = async (content: string, msgId: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopiedMessageId(msgId);
      setTimeout(() => setCopiedMessageId(null), 2000);
    } catch (error) {
      setModal({ show: true, message: '复制失败: ' + (error as Error).toString(), onConfirm: null, onCancel: null });
    }
  };

  // 删除消息（级联删除）
  const deleteMessage = async (msgId: string) => {
    try {
      const result = await httpClient.post('/api/history/messages/delete-by-id', {
        thread_id: threadId,
        content_id: msgId
      });
      // 后端返回完整树，直接更新
      dispatch(setMessagesTree(result));
    } catch (error) {
      setModal({ show: true, message: (error as Error).toString(), onConfirm: null, onCancel: null });
    }
  };

  // 生成唯一消息ID
  const generateMessageId = () => `lc_run--${crypto.randomUUID()}`;

  // 处理流式响应尾部的 state_update
  const processStateUpdateLine = (line: string): boolean => {
    try {
      const parsed = JSON.parse(line);
      if (parsed.type === 'state_update') {
        dispatch(setMessagesTree({
          messages: parsed.messages,
          active_leaf: parsed.active_leaf,
          active_path: parsed.active_path,
          branch_points: parsed.branch_points,
          tool_requests: parsed.tool_requests,
        }));
        return true;
      }
    } catch {
      // 非 JSON 或解析失败，忽略
    }
    return false;
  };

  // 重新生成消息（调用 /api/chat2/regenerate）
  const regenerateMessage = async (msgId: string) => {
    try {
      dispatch(setCurrentToolRequest(null));
      dispatch(setIsStreaming(true));

      // 立即截断当前节点之后的旧消息，避免旧内容残留
      const msgIndex = messages.findIndex(m => m.id === msgId);
      if (msgIndex !== -1) {
        dispatch(updateMessages(messages.slice(0, msgIndex + 1)));
      }

      const response = await httpClient.streamRequest('/api/chat2/regenerate', {
        method: 'POST',
        body: { msg_id: msgId, edited_content: null }
      });

      if (!response.ok) throw new Error('重新生成请求失败');

      const reader = response.body?.getReader();
      if (!reader) throw new Error('无法获取响应流');

      const decoder = new TextDecoder();
      let currentAiMessageId: string | null = null;
      let newAiResponse = "";
      let newReasoningContent = "";
      const toolCallChunksMap = new Map<number, { name?: string; args: string; id?: string }>();

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value);
        const lines = chunk.split('\n').filter(line => line.trim() !== '');

        for (const line of lines) {
          // 检查是否是 state_update
          if (processStateUpdateLine(line)) {
            dispatch(setIsStreaming(false));
            break;
          }

          try {
            const parsedChunk = JSON.parse(line);

            if (parsedChunk.interrupted) {
              console.log("重新生成被中断");
              dispatch(setIsStreaming(false));
              break;
            }

            if (parsedChunk.type === 'state_update') {
              dispatch(setMessagesTree({
                messages: parsedChunk.messages,
                active_leaf: parsedChunk.active_leaf,
                active_path: parsedChunk.active_path,
                branch_points: parsedChunk.branch_points,
                tool_requests: parsedChunk.tool_requests,
              }));
              dispatch(setIsStreaming(false));
              break;
            }

            if (!currentAiMessageId && (parsedChunk.content || parsedChunk.tool_calls?.length)) {
              const aiMessageId = generateMessageId();
              currentAiMessageId = aiMessageId;
              dispatch(createAiMessage({ id: aiMessageId }));
            }

            if (parsedChunk.content) newAiResponse += parsedChunk.content;
            if (parsedChunk.reasoning_content) newReasoningContent += parsedChunk.reasoning_content;

            if (currentAiMessageId && (parsedChunk.content || parsedChunk.reasoning_content)) {
              const updateData: any = { id: currentAiMessageId, content: newAiResponse };
              if (newReasoningContent) updateData.reasoning_content = newReasoningContent;
              dispatch(updateAiMessage(updateData));
            }

            // 上下文用量（流结束时后端通过 usage chunk 返回）
            if (parsedChunk.usage_metadata && currentAiMessageId) {
              dispatch(updateAiMessage({
                id: currentAiMessageId,
                content: newAiResponse,
                usage_metadata: parsedChunk.usage_metadata
              }));
            }

            // 流式 tool_calls（后端已合并，直接替换）
            if (parsedChunk.tool_calls && parsedChunk.tool_calls.length > 0) {
              for (const tc of parsedChunk.tool_calls) {
                const index = tc.index ?? 0;
                toolCallChunksMap.set(index, {
                  id: tc.id || toolCallChunksMap.get(index)?.id || '',
                  name: tc.function?.name || toolCallChunksMap.get(index)?.name || '',
                  args: tc.function?.arguments || ''
                });
              }

              const toolCalls: ToolCall[] = [];
              for (const [, existing] of toolCallChunksMap.entries()) {
                try {
                  JSON.parse(existing.args);
                  toolCalls.push({
                    id: existing.id || 'unknown',
                    type: 'function',
                    function: {
                      name: existing.name || 'unknown',
                      arguments: existing.args
                    }
                  });
                } catch (e) {
                  const completedArgs = tryCompleteJSON(existing.args);
                  toolCalls.push({
                    id: existing.id || 'unknown',
                    type: 'function',
                    function: {
                      name: existing.name || 'unknown',
                      arguments: completedArgs
                    }
                  });
                }
              }

              if (currentAiMessageId && toolCalls.length > 0) {
                dispatch(updateAiMessage({ id: currentAiMessageId, content: newAiResponse, tool_calls: toolCalls }));
              }

              const completeToolCalls = toolCalls.filter(tc => {
                try { JSON.parse(tc.function.arguments); return true; } catch { return false; }
              });
              if (completeToolCalls.length > 0) {
                processFileToolCalls(completeToolCalls);
              }
            }
          } catch (e) {
            console.log('无法解析chunk:', line);
          }
        }
      }
    } catch (error) {
      console.error('重新生成消息失败:', error);
      setModal({ show: true, message: (error as Error).toString(), onConfirm: null, onCancel: null });
    } finally {
      dispatch(setIsStreaming(false));
    }
  };

  // 编辑消息 - 进入编辑模式
  const editMessage = (msgId: string, messageType: 'human' | 'ai', content: string) => {
    setEditingMessageId(msgId);
    setEditingMessageType(messageType);
    setEditingContent(content);
  };

  // 确认编辑并重新生成（调用 /api/chat2/regenerate 带 edited_content）
  const confirmEdit = async (msgId: string, newContent: string) => {
    try {
      setEditingMessageId(null);
      setEditingContent('');
      dispatch(setCurrentToolRequest(null));
      dispatch(setIsStreaming(true));

      // 在前端立即替换目标消息内容，避免用户看到旧内容
      const msgIndex = messages.findIndex(m => m.id === msgId);
      if (msgIndex !== -1) {
        const updatedMessages = messages.map((m, i) =>
          i === msgIndex ? { ...m, content: newContent } : m
        );
        dispatch(updateMessages(updatedMessages.slice(0, msgIndex + 1)));
      }

      const response = await httpClient.streamRequest('/api/chat2/regenerate', {
        method: 'POST',
        body: { msg_id: msgId, edited_content: newContent }
      });

      if (!response.ok) throw new Error('编辑并重新生成请求失败');

      const reader = response.body?.getReader();
      if (!reader) throw new Error('无法获取响应流');

      const decoder = new TextDecoder();
      let currentAiMessageId: string | null = null;
      let newAiResponse = "";
      let newReasoningContent = "";
      const toolCallChunksMap = new Map<number, { name?: string; args: string; id?: string }>();

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value);
        const lines = chunk.split('\n').filter(line => line.trim() !== '');

        for (const line of lines) {
          // 检查是否是 state_update
          if (processStateUpdateLine(line)) {
            dispatch(setIsStreaming(false));
            break;
          }

          try {
            const parsedChunk = JSON.parse(line);

            if (parsedChunk.interrupted) {
              console.log("编辑后重新生成被中断");
              dispatch(setIsStreaming(false));
              break;
            }

            if (parsedChunk.type === 'state_update') {
              dispatch(setMessagesTree({
                messages: parsedChunk.messages,
                active_leaf: parsedChunk.active_leaf,
                active_path: parsedChunk.active_path,
                branch_points: parsedChunk.branch_points,
                tool_requests: parsedChunk.tool_requests,
              }));
              dispatch(setIsStreaming(false));
              break;
            }

            if (!currentAiMessageId && (parsedChunk.content || parsedChunk.tool_calls?.length)) {
              const aiMessageId = generateMessageId();
              currentAiMessageId = aiMessageId;
              dispatch(createAiMessage({ id: aiMessageId }));
            }

            if (parsedChunk.content) newAiResponse += parsedChunk.content;
            if (parsedChunk.reasoning_content) newReasoningContent += parsedChunk.reasoning_content;

            if (currentAiMessageId && (parsedChunk.content || parsedChunk.reasoning_content)) {
              const updateData: any = { id: currentAiMessageId, content: newAiResponse };
              if (newReasoningContent) updateData.reasoning_content = newReasoningContent;
              dispatch(updateAiMessage(updateData));
            }

            // 流式 tool_calls
            if (parsedChunk.tool_calls && parsedChunk.tool_calls.length > 0) {
              for (const tc of parsedChunk.tool_calls) {
                const index = tc.index ?? 0;
                toolCallChunksMap.set(index, {
                  id: tc.id || toolCallChunksMap.get(index)?.id || '',
                  name: tc.function?.name || toolCallChunksMap.get(index)?.name || '',
                  args: tc.function?.arguments || ''
                });
              }

              const toolCalls: ToolCall[] = [];
              for (const [, existing] of toolCallChunksMap.entries()) {
                try {
                  JSON.parse(existing.args);
                  toolCalls.push({
                    id: existing.id || 'unknown',
                    type: 'function',
                    function: {
                      name: existing.name || 'unknown',
                      arguments: existing.args
                    }
                  });
                } catch (e) {
                  const completedArgs = tryCompleteJSON(existing.args);
                  toolCalls.push({
                    id: existing.id || 'unknown',
                    type: 'function',
                    function: {
                      name: existing.name || 'unknown',
                      arguments: completedArgs
                    }
                  });
                }
              }

              if (currentAiMessageId && toolCalls.length > 0) {
                dispatch(updateAiMessage({ id: currentAiMessageId, content: newAiResponse, tool_calls: toolCalls }));
              }

              const completeToolCalls = toolCalls.filter(tc => {
                try { JSON.parse(tc.function.arguments); return true; } catch { return false; }
              });
              if (completeToolCalls.length > 0) {
                processFileToolCalls(completeToolCalls);
              }
            }
          } catch (e) {
            console.log('无法解析chunk:', line);
          }
        }
      }
    } catch (error) {
      console.error('编辑消息失败:', error);
      setModal({ show: true, message: (error as Error).toString(), onConfirm: null, onCancel: null });
    } finally {
      dispatch(setIsStreaming(false));
    }
  };

  // 取消编辑
  const cancelEdit = () => {
    setEditingMessageId(null);
    setEditingContent('');
  };

  // 切换分支
  const switchBranch = async (parentMsgId: string, targetMsgId: string) => {
    try {
      const result = await httpClient.post('/api/chat2/switch-branch', {
        parent_msg_id: parentMsgId,
        target_msg_id: targetMsgId
      });
      dispatch(setMessagesTree(result));
    } catch (error) {
      setModal({ show: true, message: (error as Error).toString(), onConfirm: null, onCancel: null });
    }
  };

  // 获取预览内容（第一行或前几个字）
  const getPreviewContent = (content: string): string => {
    const lines = content.split('\n');
    const firstLine = lines[0]?.trim() || '';
    if (firstLine.length > 50) {
      return firstLine.substring(0, 50) + '...';
    }
    return firstLine || '...';
  };

  // 渲染用户消息内容，高亮@文件路径
  const renderUserMessageContent = (content: string) => {
    if (!content) return null;
    
    // 匹配 @文件路径 的模式
    const parts = content.split(/(@[^\s\n]+)/g);
    
    return parts.map((part, index) => {
      if (part.startsWith('@') && part.length > 1) {
        return (
          <span key={index} className="text-theme-gray3">
            {part}
          </span>
        );
      }
      return <span key={index}>{part}</span>;
    });
  };

  // 获取消息所在分支点信息
  const getBranchPointForMsg = (msgId: string): BranchPoint | undefined => {
    const result = branchPoints.find(bp => bp.variants.includes(msgId));
    return result;
  };

  // 当消息列表变化时自动滚动到底部
  const scrollRef = useRef(messages.length);
  if (messages.length !== scrollRef.current) {
    scrollRef.current = messages.length;
    setTimeout(scrollToBottom, 0);
  }

  // 渲染消息
  const renderMessage = (msg: Message) => {
    const isUser = msg.role === 'user';
    const isToolResult = msg.role === 'tool';
    const isEditing = editingMessageId === msg.id;
    const bpInfo = isUser ? getBranchPointForMsg(msg.id) : undefined;
    // 工具结果消息独立渲染
    if (isToolResult) {
      const isExpanded = expandedToolResults.has(msg.id);
      const previewContent = getPreviewContent(msg.content || '');
      
      return (
        <div
          key={msg.id}
          className="flex flex-col w-[80%] self-start bg-theme-gray1 border border-theme-green p-2.5 rounded-medium break-words overflow-wrap break-word"
        >
          <div className="flex items-center">
            <div className="flex items-center cursor-pointer" onClick={() => toggleToolResultExpand(msg.id)}>
              <FontAwesomeIcon icon={isExpanded ? faAngleUp : faAngleRight} className="text-theme-green hover:text-theme-white text-xs mr-2" />
              <span className="font-bold text-[0.9em] text-theme-white">工具</span>
            </div>
          </div>
          <div className="leading-[1.4] overflow-wrap break-word break-words text-theme-white mt-1">
            {isExpanded ? (
              <div className="whitespace-pre-wrap">{msg.content}</div>
            ) : (
              <div className="text-sm">{previewContent}</div>
            )}
          </div>
        </div>
      );
    }
    
    // 用户消息、AI消息
    const usageMetadata = msg.role === 'assistant' ? msg.usage_metadata : null;
    const inputTokens = usageMetadata?.input_tokens || 0;
    const outputTokens = usageMetadata?.output_tokens || 0;
    
    return (
      <div
        key={msg.id}
        className={`flex flex-col w-[80%] ${
          isUser ? 'self-end' : 'self-start'
        }`}
      >
        {/* 消息气泡 */}
        <div
          className={`flex flex-col w-full p-2.5 rounded-medium break-words overflow-wrap break-word ${
            isUser
              ? 'bg-theme-green1 text-theme-white'
              : 'bg-theme-gray2 text-theme-white'
          }`}
        >
          <div className="flex items-center justify-between mb-1">
            <div className="font-bold text-[0.9em]">
              {isEditing ? '编辑消息' : (isUser ? '用户' : 'AI')}
            </div>
          </div>
          <div className="leading-[1.4] overflow-wrap break-word break-words">
            {isEditing ? (
              <div className="flex flex-col gap-2">
                <textarea
                  className="w-full bg-theme-gray1 text-theme-white p-2 rounded-small border border-theme-gray3 focus:border-theme-green outline-none resize-none"
                  rows={6}
                  value={editingContent}
                  onChange={(e) => setEditingContent(e.target.value)}
                  autoFocus
                />
                <div className="flex justify-end gap-2">
                  <button
                    className="px-4 py-2 rounded-small bg-theme-gray3 text-theme-white hover:bg-theme-gray4 transition-colors"
                    onClick={cancelEdit}
                  >
                    取消
                  </button>
                  <button
                    className="px-4 py-2 rounded-small bg-theme-green text-theme-white hover:bg-theme-green1 transition-colors"
                    onClick={() => confirmEdit(msg.id, editingContent)}
                  >
                    确定并重新生成
                  </button>
                </div>
              </div>
            ) : (
              <div>
                {isUser ? (
                  <div className="whitespace-pre-wrap">{renderUserMessageContent(msg.content || '')}</div>
                ) : (
                  <div>
                    {msg.role === 'assistant' && Boolean(msg.additional_kwargs?.reasoning_content) && (
                      <div className="mt-2 p-2 bg-black/20 rounded-small">
                        <div className="flex items-center gap-2">
                          <FontAwesomeIcon
                            icon={expandedReasonings.has(msg.id) ? faAngleUp : faAngleRight}
                            className="text-xs text-theme-green cursor-pointer hover:text-theme-white"
                            onClick={() => toggleReasoningExpand(msg.id)}
                          />
                          <span className="font-bold text-theme-green">思维链</span>
                        </div>
                        {expandedReasonings.has(msg.id) && (
                          <div className="mt-1 text-[0.8em] text-theme-white whitespace-pre-wrap break-words">
                            {msg.additional_kwargs?.reasoning_content as string}
                          </div>
                        )}
                      </div>
                    )}
                    <div className="whitespace-pre-wrap">{msg.content}</div>
                    {msg.role === 'assistant' && msg.tool_calls && msg.tool_calls.length > 0 && (
                      <div className="mt-2 p-2 bg-black/20 rounded-small">
                        {msg.tool_calls!.map((toolCall, toolIndex) => {
                          const toolKey = `${msg.id}-${toolIndex}`;
                          const isExpanded = !expandedTools.has(toolKey);
                          const tcName = toolCall.function?.name || '';
                          const tcArgsStr = toolCall.function?.arguments || '';
                          let parsedArgs: any = {};
                          let isArgsValid = false;
                          try {
                            parsedArgs = JSON.parse(tcArgsStr);
                            isArgsValid = true;
                          } catch { /* 参数未完整，使用原始字符串 */ }
                          const path = isArgsValid && parsedArgs && typeof parsedArgs === 'object' && 'path' in parsedArgs ? parsedArgs.path : null;
                          
                          return (
                            <div key={toolIndex} className="mb-1.5 p-1 bg-black/10 rounded-small">
                              <div className="flex items-center gap-2">
                                <FontAwesomeIcon
                                  icon={isExpanded ? faAngleUp : faAngleRight}
                                  className="text-xs text-theme-green cursor-pointer hover:text-theme-white"
                                  onClick={() => toggleToolExpand(msg.id, toolIndex)}
                                />
                                <span className="font-bold text-theme-green">
                                  {availableTools[tcName]?.name || tcName || '未知工具'}
                                </span>
                                {path && (
                                  <span className="text-xs text-theme-gray3">
                                    {path}
                                  </span>
                                )}
                              </div>
                              {isExpanded && tcArgsStr && (
                                <div className="mt-1 text-[0.8em] text-theme-white whitespace-pre-wrap break-words">
                                  {!isArgsValid ? (
                                    `加载中... ${tcArgsStr}`
                                  ) : (() => {
                                      const content = parsedArgs.content;
                                      if (content !== undefined) {
                                        return content;
                                      }
                                      const result: Record<string, any> = {};
                                      for (const [key, value] of Object.entries(parsedArgs)) {
                                        if (key !== 'content') {
                                          result[key] = value;
                                        }
                                      }
                                      return JSON.stringify(result, null, 2);
                                    })()}
                                </div>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
        
        {/* 气泡下方的操作栏（编辑模式下隐藏） */}
        {!isEditing && (
          <div className="flex items-center justify-between mt-1 px-1">
            {/* 按钮组 */}
            <div className="flex gap-2">
              <button
                className="text-xs flex items-center gap-1 text-theme-gray3 hover:text-theme-green transition-colors"
                onClick={() => copyMessage(msg.content || '', msg.id)}
                title={copiedMessageId === msg.id ? "已复制" : "复制"}
              >
                <FontAwesomeIcon icon={copiedMessageId === msg.id ? faCheck : faCopy} />
              </button>
              {isUser && !isInterrupted && (
                <button
                  className="text-xs flex items-center gap-1 text-theme-gray3 hover:text-theme-green transition-colors"
                  onClick={() => regenerateMessage(msg.id)}
                  title="重新生成"
                >
                  <FontAwesomeIcon icon={faRotateRight} />
                </button>
              )}
              {!isInterrupted && (
                <button
                  className="text-xs flex items-center gap-1 text-theme-gray3 hover:text-theme-red transition-colors"
                  onClick={() => deleteMessage(msg.id)}
                  title="删除"
                >
                  <FontAwesomeIcon icon={faTrash} />
                </button>
              )}
              {!isInterrupted && (
                <button
                  className="text-xs flex items-center gap-1 text-theme-gray3 hover:text-theme-green transition-colors"
                  onClick={() => editMessage(msg.id, isUser ? 'human' : 'ai', msg.content || '')}
                  title="编辑"
                >
                  <FontAwesomeIcon icon={faEdit} />
                </button>
              )}
            </div>
            
            {/* 右侧上下文信息 */}
            {usageMetadata && (inputTokens > 0 || outputTokens > 0) && (
              <div className="text-xs text-theme-gray3">
                ↑ {inputTokens} ↓ {outputTokens}
              </div>
            )}
          </div>
        )}

        {/* 分支翻页器（仅对用户消息且是分支点显示） */}
        {isUser && bpInfo && bpInfo.total > 1 && !isEditing && (
          <div className="flex items-center justify-center gap-2 mt-1">
            <button
              className="text-xs text-theme-gray3 hover:text-theme-green transition-colors disabled:opacity-30"
              disabled={bpInfo.current_index === 0}
              onClick={() => {
                const prevIdx = bpInfo.current_index - 1;
                const target = bpInfo.variants[prevIdx];
                if (prevIdx >= 0 && target) {
                  switchBranch(bpInfo.at_msg_id, target);
                }
              }}
            >
              <FontAwesomeIcon icon={faChevronLeft} />
            </button>
            <span className="text-xs text-theme-gray3">
              {bpInfo.current_index + 1}/{bpInfo.total}
            </span>
            <button
              className="text-xs text-theme-gray3 hover:text-theme-green transition-colors disabled:opacity-30"
              disabled={bpInfo.current_index >= bpInfo.total - 1}
              onClick={() => {
                const nextIdx = bpInfo.current_index + 1;
                const target = bpInfo.variants[nextIdx];
                if (nextIdx < bpInfo.total && target) {
                  switchBranch(bpInfo.at_msg_id, target);
                }
              }}
            >
              <FontAwesomeIcon icon={faChevronRight} />
            </button>
          </div>
        )}
      </div>
    );
  };

  if (!chatState?.values?.messages) {
    return (
      <div className="flex-1 overflow-y-auto p-2.5 flex flex-col items-center justify-center text-theme-gray3">
        <p>选择或创建一个会话开始对话</p>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto p-2.5 flex flex-col relative">
      <div className="flex-1 overflow-y-auto mt-2.5 flex flex-col gap-2">
        {messages.map(renderMessage)}
        <div ref={messagesEndRef} />
      </div>
      {/* 模态框管理模块 */}
      {modal.show && (
        <UnifiedModal
          message={modal.message}
          buttons={[
            { text: '确定', onClick: modal.onConfirm || (() => setModal({ show: false, message: '', onConfirm: null, onCancel: null })), className: 'bg-theme-green' },
            { text: '取消', onClick: modal.onCancel || (() => setModal({ show: false, message: '', onConfirm: null, onCancel: null })), className: 'bg-theme-gray3' }
          ]}
        />
      )}
    </div>
  );
};

export default MessageDisplayPanel;
