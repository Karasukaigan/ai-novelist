import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faPaperPlane, faStop } from '@fortawesome/free-solid-svg-icons';
import { useDispatch, useSelector } from 'react-redux';
import { useState, useRef } from 'react';
import type { RootState } from '../../types';
import {
  addUserMessage,
  createAiMessage,
  updateAiMessage,
  setState,
  setMessage,
  setSelectedThreadId,
  setIsStreaming,
  setCurrentToolRequest,
  setMessagesTree,
} from '../../store/chat';
import type { ToolCall } from '../../types/langgraph';
import { tryCompleteJSON } from '../../utils/jsonUtils';
import { store } from '../../store/store';
import httpClient from '../../utils/httpClient';
import { useFileToolHandler } from '../../utils/fileToolHandler';
import { useFilePathAutocomplete } from './hooks/useFilePathAutocomplete';
import { FilePathAutocomplete } from './FilePathAutocomplete';

const MessageInputPanel = () => {
  const dispatch = useDispatch();
  const { processFileToolCalls } = useFileToolHandler();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  
  // 从Redux获取状态
  const chatSliceState = useSelector((state: RootState) => state.chatSlice);
  const message = chatSliceState.message;
  const selectedThreadId = chatSliceState.selectedThreadId;
  const isStreaming = chatSliceState.isStreaming;

  // 本地错误状态
  const [error, setError] = useState('');
  
  // 文件路径补全功能
  const {
    isOpen: isAutocompleteOpen, // 重命名
    filteredPaths,
    selectedIndex,
    query,
    handleInputChange: handleAutocompleteInput,
    handleKeyDown: handleAutocompleteKeyDown,
    selectPath,
    closeAutocomplete
  } = useFilePathAutocomplete(message, (newMessage) => {
    dispatch(setMessage(newMessage));
    // 设置光标位置到选中路径之后
    setTimeout(() => {
      if (textareaRef.current) {
        const atIndex = newMessage.lastIndexOf('@');
        if (atIndex !== -1) {
          const endPos = atIndex + 1 + (filteredPaths[selectedIndex] || '').length;
          textareaRef.current.setSelectionRange(endPos, endPos);
          textareaRef.current.focus();
        }
      }
    }, 0);
  }); // 传给hook的回调函数，让hook能更新redux中的消息状态
  
  // 生成唯一消息ID
  const generateMessageId = () => {
    const uuid = crypto.randomUUID();
    return `lc_run--${uuid}`;
  };

  // 生成随机thread_id
  const generateThreadId = () => {
    return `thread_${Date.now()}`;
  };

  // 停止流式传输
  const handleStopStreaming = async () => {
    if (!selectedThreadId) return;

    try {
      await httpClient.post('/api/chat/interrupt-stream', { thread_id: selectedThreadId });
      console.log('流式传输已中断');
    } catch (error) {
      console.error('中断流式传输失败:', error);
    }
    
    // 立即更新状态，关闭中断按钮，恢复发送按钮
    dispatch(setIsStreaming(false));
  };

  // 发送消息到后端（chat_api2）
  const sendMessage = async function* (messages: any[]) {
    try {
      const response = await httpClient.streamRequest('/api/chat2/message', {
        method: 'POST',
        body: { messages }
      } as any);

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const reader = response.body!.getReader();
      const decoder = new TextDecoder();

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        yield chunk;
      }
    } catch (error) {
      console.log(error);
      throw error;
    }
  };

  // 处理发送消息
  const handleSendMessage = async () => {
    if (!message.trim()) return;

    const inputMessage = message.trim();
    
    setError('');

    const userMessageId = generateMessageId();
    dispatch(setMessage(''));

    // 如果没有选中thread_id，则创建新的（通过 config API 写入 thread_id）
    let actualThreadId = selectedThreadId;
    if (!actualThreadId) {
      try {
        const newThreadId = generateThreadId();
        await httpClient.post('/api/config/store', { key: 'thread_id', value: newThreadId });
        actualThreadId = newThreadId;
        dispatch(setSelectedThreadId(actualThreadId));
        console.log("创建新会话成功，thread_id:", actualThreadId);
      } catch (error) {
        console.error('创建新会话失败:', error);
        return;
      }
    }

    // 确保 state.state 已初始化
    if (!chatSliceState.state) {
      dispatch(setState({
        values: { messages: [], summary: '' },
        next: null,
        config: { configurable: { thread_id: actualThreadId || '' } },
        metadata: { source: 'input', step: 0, parents: {}, user_id: '' },
        created_at: new Date().toISOString(),
        parent_config: null,
        tasks: [],
        interrupts: []
      }));
    }

    dispatch(addUserMessage({ id: userMessageId, content: inputMessage }));

    try {
      // 开始流式传输
      dispatch(setIsStreaming(true));
      const result = sendMessage(store.getState().chatSlice.state?.values?.messages || []);
      let currentAiMessageId: string | null = null;
      let newAiResponse = "";
      let newReasoningContent = "";
      const toolCallChunksMap = new Map<number, { name?: string; args: string; id?: string }>();

      for await (const chunk of result) {
        const lines = chunk.split('\n').filter(line => line.trim() !== '');
        
        for (const line of lines) {
          try {
            const parsedChunk = JSON.parse(line);

            // 处理流式传输中断信号
            if (parsedChunk.interrupted) {
              console.log("流式传输已被中断");
              dispatch(setIsStreaming(false));
              break;
            }

            // 处理统一状态更新（消息树 + 工具请求）
            if (parsedChunk.type === 'state_update') {
              if (parsedChunk.messages) {
                dispatch(setMessagesTree({
                  messages: parsedChunk.messages,
                  active_leaf: parsedChunk.active_leaf,
                  active_path: parsedChunk.active_path,
                  branch_points: parsedChunk.branch_points,
                }));
              }
              // 从 state_update 中检查待审批的工具请求
              if (parsedChunk.tool_requests) {
                const trMap: Record<string, any> = parsedChunk.tool_requests;
                const pendingEntry = Object.entries(trMap).find(([_, tr]) => tr.approved === null);
                if (pendingEntry) {
                  const [tool_call_id, info] = pendingEntry;
                  dispatch(setCurrentToolRequest({
                    tool_call_id,
                    tool_name: info.tool_name,
                    arguments: info.arguments,
                    notified: true,
                    approved: null,
                    user_extra: null,
                    result: null,
                  }));
                } else {
                  dispatch(setCurrentToolRequest(null));
                }
              }
              continue;
            }

            // 首次收到内容时创建 AI message（tool_calls 由 tool_requests 机制处理）
            if (!currentAiMessageId && parsedChunk.content) {
              const aiMessageId = generateMessageId();
              currentAiMessageId = aiMessageId;
              dispatch(createAiMessage({ id: aiMessageId }));
            }

            if (parsedChunk.content) {
              newAiResponse += parsedChunk.content;
            }

            if (parsedChunk.reasoning_content) {
              newReasoningContent += parsedChunk.reasoning_content;
            }

            // 实时更新流式渲染
            if (currentAiMessageId && (parsedChunk.content || parsedChunk.reasoning_content)) {
              const updateData: any = {
                id: currentAiMessageId,
                content: newAiResponse
              };
              if (newReasoningContent) {
                updateData.reasoning_content = newReasoningContent;
              }
              dispatch(updateAiMessage(updateData));
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

              // 传原始 args（未补全）给 fileToolHandler，让 processFileToolCalls 内部
              // 通过 JSON.parse 失败 → tryCompleteJSON 补全 → isPartial=true → 路径稳定器处理
              const rawToolCalls: ToolCall[] = [];
              for (const [, existing] of toolCallChunksMap.entries()) {
                rawToolCalls.push({
                  id: existing.id || 'unknown',
                  type: 'function',
                  function: {
                    name: existing.name || 'unknown',
                    arguments: existing.args
                  }
                });
              }
              if (rawToolCalls.length > 0) {
                processFileToolCalls(rawToolCalls);
              }
            }
          } catch (e) {
            console.log('[STREAM] 无法解析chunk:', line);
          }
        }
      }
      
      // 流式传输结束，清除状态
      dispatch(setIsStreaming(false));
    } catch (err) {
      setError(err instanceof Error ? err.message : '发送消息失败');
      dispatch(setMessage(inputMessage));
      
      // 流式传输出错，清除状态
      dispatch(setIsStreaming(false));
    }
    
  };

  // 处理键盘事件
  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // 先处理文件路径补全的键盘事件
    if (handleAutocompleteKeyDown(e)) {
      return;
    }
    
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSendMessage();
    }
  };

  // 处理输入变化
  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = e.target.value;
    const cursorPos = e.target.selectionStart;
    dispatch(setMessage(value));
    handleAutocompleteInput(value, cursorPos);
  };

  // 处理选中文件路径
  const handleSelectPath = (path: string) => {
    const newMessage = selectPath(path);
    dispatch(setMessage(newMessage));
    // 设置光标位置
    setTimeout(() => {
      if (textareaRef.current) {
        const atIndex = newMessage.indexOf('@' + path);
        if (atIndex !== -1) {
          const endPos = atIndex + 1 + path.length;
          textareaRef.current.setSelectionRange(endPos, endPos);
          textareaRef.current.focus();
        }
      }
    }, 0);
  };


  // 渲染带高亮的文本内容
  const renderHighlightedContent = () => {
    if (!message) return <br />;
    
    // 匹配 @路径 的模式
    const parts = message.split(/(@[^\s\n]+)/g);
    
    return parts.map((part, index) => {
      if (part.startsWith('@') && part.length > 1) {
        return (
          <span key={index} className="bg-theme-gray3 text-theme-green rounded-sm px-0.5">
            {part}
          </span>
        );
      }
      return <span key={index}>{part}</span>;
    });
  };

  return (
    <>
      {/* 输入区域 */}
      <div className="h-[15%] p-2.5 border border-theme-gray3 flex flex-col">
        {/* 输入框占位 */}
        <div className="flex w-full flex-1 relative overflow-visible">
          {/* 文件路径补全下拉框 */}
          <FilePathAutocomplete
            isOpen={isAutocompleteOpen}
            paths={filteredPaths}
            selectedIndex={selectedIndex}
            query={query}
            onSelect={handleSelectPath}
            onClose={closeAutocomplete}
          />
          {/* 高亮层 - 显示带背景色的文本 */}
          <pre
            className="absolute inset-0 m-0 p-0 bg-transparent text-transparent border-none rounded-small resize-none font-inherit text-[14px] box-border flex-1 min-w-0 pointer-events-none overflow-hidden whitespace-pre-wrap break-words leading-[normal]"
            style={{ fontFamily: 'inherit' }}
            aria-hidden="true"
          >
            {renderHighlightedContent()}
          </pre>
          <textarea
            ref={textareaRef}
            className="bg-transparent text-theme-white border-none rounded-small resize-none font-inherit text-[14px] box-border flex-1 min-w-0 focus:outline-none"
            placeholder="输入@引用文件，同时按下shift+回车可换行"
            rows={3}
            value={message}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
          />
          {isStreaming ? (
            <button
              className="bg-transparent border-none cursor-pointer text-[16px] p-0 self-end flex items-center justify-center hover:text-theme-red"
              onClick={handleStopStreaming}
            >
              <FontAwesomeIcon icon={faStop} />
            </button>
          ) : (
            <button
              className="bg-transparent border-none cursor-pointer text-[16px] p-0 self-end flex items-center justify-center hover:text-theme-green disabled:text-theme-white disabled:cursor-not-allowed"
              onClick={handleSendMessage}
              disabled={!message.trim()}
            >
              <FontAwesomeIcon icon={faPaperPlane} />
            </button>
          )}
        </div>
      </div>

      {/* 错误弹窗 */}
      {error && (
        <div className="fixed inset-0 flex items-center justify-center z-50 bg-black bg-opacity-50">
          <div className="bg-theme-black border border-theme-gray3 rounded-small p-4 max-w-md w-full mx-4">
            <div className="text-theme-green text-lg mb-2">错误</div>
            <div className="text-theme-white text-sm mb-4">{error}</div>
            <button
              className="w-full bg-theme-green text-theme-white border-none rounded-small py-2 cursor-pointer hover:bg-theme-white hover:text-theme-green transition-all"
              onClick={() => setError('')}
            >
              确定
            </button>
          </div>
        </div>
      )}
    </>
  );
};

export default MessageInputPanel;
