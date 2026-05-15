import { useState } from 'react';
import { useDispatch, useSelector } from 'react-redux';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faPlus, faFileLines } from '@fortawesome/free-solid-svg-icons';
import ModeSelectorPanel from './mode-selector/ModeSelector';
import ModePanel from './mode-selector/ModePanel';
import AutoApprovePanel from './auto-approve/AutoApproveButton';
import ModelSelectorPanel from './ModelSelectorPanel';
import TwoStepRagSelector from './two-step-rag/TwoStepRagSelector';
import TwoStepRagPanel from './two-step-rag/TwoStepRagPanel';
import ContextProgressBar from './ContextProgressBar';
import MessageInputPanel from './MessageInputPanel';
import MiddlePart from './MiddlePart';
import { setState, setSelectedThreadId, setCurrentToolRequest, setMessage, setMessagesTree, createAiMessage, updateAiMessage, setIsStreaming } from '../../store/chat';
import type { RootState } from '../../types';
import httpClient from '../../utils/httpClient';

const ChatPanel = () => {
  const dispatch = useDispatch();
  const selectedThreadId = useSelector((state: RootState) => state.chatSlice.selectedThreadId);
  const summaries = useSelector((state: RootState) => state.chatSlice.summaries);
  const latestSummary = summaries.length > 0 ? (summaries[summaries.length - 1]?.content ?? "") : "";
  const [isSummarizing, setIsSummarizing] = useState(false);

  // 创建新会话
  const handleNewThread = () => {
    dispatch(setState(null));
    dispatch(setSelectedThreadId(null));
    dispatch(setCurrentToolRequest(null));
    dispatch(setMessage(''));
    console.log("回到初始状态");
  };
  // 压缩上下文
  const handleSummarize = async () => {
    if (!selectedThreadId) return;
    setIsSummarizing(true);
    dispatch(setIsStreaming(true));  // 禁用输入栏
    try {
      const resp = await httpClient.post('/api/chat2/summarize', { thread_id: selectedThreadId });
      if (resp) {
        dispatch(setMessagesTree({
          messages: resp.messages,
          active_leaf: resp.active_leaf,
          active_path: resp.active_path,
          branch_points: resp.branch_points,
          summaries: resp.summaries,
        }));
        // 显示临时提示消息
        const tempId = `msg-temp-summarize-${Date.now()}`;
        dispatch(createAiMessage({ id: tempId }));
        dispatch(updateAiMessage({
          id: tempId,
          content: "【压缩成功】，具体消息可将鼠标悬浮在压缩按钮查看",
        }));
      }
    } catch (e) {
      console.error('压缩上下文失败:', e);
    } finally {
      setIsSummarizing(false);
      dispatch(setIsStreaming(false));  // 恢复输入栏
    }
  };

  return (
    <div className="flex flex-col h-full relative">
      {/* 顶部区域 */}
      <div className="h-[5%] w-full flex justify-center items-center p-1 border-b border-theme-gray3 gap-5">
        <ModelSelectorPanel />

        {/* 压缩上下文按钮 */}
        <button
          className="bg-theme-black text-theme-white rounded-small w-[2vw] h-[3.5vh] text-lg font-bold flex items-center justify-center border-0 transition-all hover:border hover:border-theme-green hover:text-theme-green disabled:bg-theme-gray1 disabled:cursor-not-allowed disabled:opacity-60"
          title={latestSummary || "压缩上下文"}
          onClick={handleSummarize}
          disabled={isSummarizing || !selectedThreadId}
        >
          {isSummarizing ? (
            <div className="animate-spin rounded-full h-4 w-4 border-2 border-theme-white border-t-transparent"></div>
          ) : (
            <FontAwesomeIcon icon={faFileLines} />
          )}
        </button>

        {/* 创建新会话按钮 */}
        <button
          className="bg-theme-black text-theme-white rounded-small w-[2vw] h-[3.5vh] text-lg font-bold flex items-center justify-center border-0 transition-all hover:border hover:border-theme-green hover:text-theme-green"
          title="创建新会话"
          onClick={handleNewThread}
        >
          <FontAwesomeIcon icon={faPlus} />
        </button>
      </div>

      {/* 上下文进度条 */}
      <ContextProgressBar />

      {/* 中间部分 - 消息显示区域/历史消息栏 */}
      <MiddlePart />

      {/* 输入区域 */}
      <MessageInputPanel />

      {/* 底部工具栏 */}
      <div className="w-full flex p-2.5 border-t border-theme-gray1 relative gap-2">
        <ModeSelectorPanel />
        <TwoStepRagSelector />
        <AutoApprovePanel />
      </div>

      {/* 两步RAG面板 */}
      <TwoStepRagPanel />

      {/* 模式面板 */}
      <ModePanel />

    </div>
  );
};

export default ChatPanel;
