import { useSelector, useDispatch, useStore } from 'react-redux';
import { useEffect, useRef } from 'react';
import type { RootState } from '../../types';
import type { ToolCall, ToolRequestData } from '../../types/langgraph';
import { setCurrentToolRequest, setIsStreaming, createAiMessage, updateAiMessage, addUserMessage, addToolMessage, setMessage, setMessagesTree } from '../../store/chat';
import { exitDiffMode, saveTabContent, decreaseTab, clearAiSuggestContent } from '../../store/editor';
import { FILE_TOOLS, useFileToolHandler } from '../../utils/fileToolHandler';
import httpClient from '../../utils/httpClient';

const ToolRequestPanel = () => {
  const dispatch = useDispatch();
  const store = useStore();
  const currentToolRequest = useSelector((state: RootState) => state.chatSlice.currentToolRequest);
  const message = useSelector((state: RootState) => state.chatSlice.message);
  const autoApproveEnabled = useSelector((state: RootState) => state.chatSlice.autoApproveEnabled);
  const autoApproveRef = useRef(false);

  // 生成唯一消息ID
  const generateMessageId = () => {
    const uuid = crypto.randomUUID();
    return `lc_run--${uuid}`;
  };

  const { processFileToolCalls } = useFileToolHandler();

  // 处理工具调用
  const handleFunctionCalling = async (approved: boolean) => {
    if (!currentToolRequest) return;

    const extra = message || '';
    const toolName = currentToolRequest.tool_name;
    const argsStr = currentToolRequest.arguments;

    dispatch(setCurrentToolRequest(null));
    dispatch(setMessage(''));

    // 清理文件工具的差异对比视图
    if (argsStr && FILE_TOOLS.includes(toolName)) {
      try {
        const args = JSON.parse(argsStr);
        const path: string | undefined = args.path;
        if (path) {
          dispatch(exitDiffMode({ id: path }));
          if (approved) {
            // 批准：同步 currentData 到 backUp
            dispatch(saveTabContent({ id: path }));
          } else {
            // 拒绝：关闭标签
            dispatch(decreaseTab({ tabId: path }));
          }
          dispatch(clearAiSuggestContent({ id: path }));
        }
      } catch (e) {
        console.error('解析工具参数失败:', e);
      }
    }

    // 如果用户有输入，先加入聊天
    if (extra) {
      dispatch(addUserMessage({ id: generateMessageId(), content: extra }));
    }

    try {
      dispatch(setIsStreaming(true));

      const response = await httpClient.streamRequest('/api/chat2/function_calling', {
        method: 'POST',
        body: {
          tool_call_id: currentToolRequest.tool_call_id,
          approved,
          user_extra: extra,
        }
      } as any);

      if (!response.ok) throw new Error('工具调用请求失败');

      const reader = response.body!.getReader();
      const decoder = new TextDecoder();
      let currentAiMessageId: string | null = null;
      let newAiResponse = '';
      const toolCallChunksMap = new Map<number, { name?: string; args: string; id?: string }>();

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        const lines = chunk.split('\n').filter(line => line.trim() !== '');

        for (const line of lines) {
          try {
            const parsed = JSON.parse(line);

            if (parsed.interrupted) {
              dispatch(setIsStreaming(false));
              break;
            }

            if (parsed.type === 'state_update') {
              if (parsed.messages) {
                dispatch(setMessagesTree({
                  messages: parsed.messages,
                  active_leaf: parsed.active_leaf,
                  active_path: parsed.active_path,
                  branch_points: parsed.branch_points,
                }));
              }
              // 从 state_update 中检查待审批的工具请求
              if (parsed.tool_requests) {
                const trMap: Record<string, any> = parsed.tool_requests;
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

            if (parsed.content !== undefined || parsed.tool_calls) {
              // AI 流式回复
              if (!currentAiMessageId && (parsed.content || parsed.tool_calls?.length)) {
                currentAiMessageId = `temp-ai-${Date.now()}`;
                dispatch(createAiMessage({ id: currentAiMessageId }));
              }

              if (parsed.content) {
                newAiResponse += parsed.content;
                if (currentAiMessageId) {
                  dispatch(updateAiMessage({
                    id: currentAiMessageId,
                    content: newAiResponse,
                  }));
                }
              }

              // 流式 tool_calls：实时解析参数并打开差异对比视图
              if (parsed.tool_calls && parsed.tool_calls.length > 0) {
                for (const tc of parsed.tool_calls) {
                  const index = tc.index ?? 0;
                  toolCallChunksMap.set(index, {
                    id: tc.id || toolCallChunksMap.get(index)?.id || '',
                    name: tc.function?.name || toolCallChunksMap.get(index)?.name || '',
                    args: tc.function?.arguments || ''
                  });
                }

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
            }
          } catch (e) {
            console.log('无法解析chunk:', line);
          }
        }
      }

      dispatch(setIsStreaming(false));
    } catch (error) {
      console.error('工具调用失败:', error);
      dispatch(setIsStreaming(false));
    }
  };

  // 自动批准
  useEffect(() => {
    if (autoApproveEnabled && currentToolRequest && !autoApproveRef.current) {
      autoApproveRef.current = true;
      const timer = setTimeout(() => {
        handleFunctionCalling(true);
        autoApproveRef.current = false;
      }, 1000);
      return () => clearTimeout(timer);
    } else if (!currentToolRequest) {
      autoApproveRef.current = false;
    }
  }, [currentToolRequest, autoApproveEnabled]);

  return (
    <div className="w-full bg-theme-gray1 p-2 space-y-2">
      {/* 工具请求审批 */}
      {currentToolRequest && (
        <>
          <div className="text-theme-green text-[13px] font-medium">
            工具请求: {currentToolRequest.tool_name}
          </div>
          <div className="flex gap-4">
            <button
              className="flex-1 text-theme-white border-none rounded-small py-2 px-4 text-[13px] font-medium cursor-pointer transition-all hover:border-1 hover:border-solid hover:border-theme-green hover:text-theme-green"
              onClick={() => handleFunctionCalling(true)}
            >
              批准
            </button>
            <button
              className="flex-1 text-theme-white border-none rounded-small py-2 px-4 text-[13px] font-medium cursor-pointer transition-all hover:border-1 hover:border-solid hover:border-theme-red hover:text-theme-red"
              onClick={() => handleFunctionCalling(false)}
            >
              取消
            </button>
          </div>
        </>
      )}

      {!currentToolRequest && null}
    </div>
  );
};

export default ToolRequestPanel;
