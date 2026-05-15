// OpenAI/LiteLLM 消息格式类型定义

// 工具调用（OpenAI 标准格式）
export interface ToolCall {
  id: string;
  type: string;
  function: {
    name: string;
    arguments: string;  // JSON 字符串
  };
}

// 分支点信息（后端计算后返回）
export interface BranchPoint {
  at_msg_id: string;
  variants: string[];
  active: string;
  current_index: number;
  total: number;
}

// 无效工具调用
export interface InvalidToolCall {
  name?: string;
  id?: string;
  args?: string;
  error?: string;
}

// 使用元数据
export interface UsageMetadata {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  input_token_details?: {
    cache_read?: number;
    cached_tokens?: number;
  };
  output_token_details?: Record<string, unknown>;
}

// 响应元数据
export interface ResponseMetadata {
  finish_reason?: string;
  model_name?: string;
  system_fingerprint?: string;
  model_provider?: string;
}

// OpenAI 格式消息
export interface OpenAIMessage {
  role: 'user' | 'assistant' | 'tool' | 'system';
  content: string;
  id: string;
  parent_id?: string | null;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  name?: string;
  additional_kwargs?: Record<string, unknown>;
  response_metadata?: ResponseMetadata;
  usage_metadata?: UsageMetadata;
  invalid_tool_calls?: InvalidToolCall[];
}

// 消息联合类型
export type Message = OpenAIMessage;

// 中断值
export interface InterruptValue {
  tool_name: string;
  tool_display_name?: string;
  description?: string;
  question?: string;
  parameters?: Record<string, unknown>;
}

// 中断信息
export interface Interrupt {
  id: string;
  value: InterruptValue;
}

// 工具请求数据（来自后端 tool_requests 表）
export interface ToolRequestData {
  tool_call_id: string;
  tool_name: string;
  arguments?: string;
  notified: boolean;
  approved: boolean | null;
  user_extra: string | null;
  result: { success: boolean; detail: string } | null;
}

// 任务
export interface PregelTask {
  id: string;
  name: string;
  path: string[];
  error: any;
  interrupts: Interrupt[];
  state: any;
  result: any;
}

// 配置
export interface Config {
  configurable: {
    thread_id: string;
    checkpoint_ns?: string;
    checkpoint_id?: string;
    user_id?: string;
  };
}

// 元数据
export interface StateMetadata {
  source: string;
  step: number;
  parents: Record<string, unknown>;
  user_id: string;
}

// LangGraph State（保留结构但消息改为OpenAI格式）
export interface LangGraphState {
  values: {
    messages: Message[];
    summary: string;
  };
  next: string[] | null;
  config: Config;
  metadata: StateMetadata;
  created_at: string;
  parent_config: Config | null;
  tasks: PregelTask[];
  interrupts: Interrupt[];
}

// 流式传输的chunk类型
export interface StreamChunk {
  type?: string;
  content?: string;
  tool_calls?: ToolCall[];
  id?: string;
  name?: string | null;
  additional_kwargs?: Record<string, unknown>;
  response_metadata?: ResponseMetadata;
  usage_metadata?: UsageMetadata | null;
  invalid_tool_calls?: InvalidToolCall[];
  tool_call_chunks?: ToolCallChunk[];
  chunk_position?: string | null;
  // 流式传输ID（用于中断流式传输）
  stream_id?: string;
  // 是否被中断
  interrupted?: boolean;
}

// 工具调用chunk（用于流式传输）
export interface ToolCallChunk {
  name?: string | null;
  args?: string | null;
  id?: string | null;
  index?: number;
  type?: string;
}

// 上下文压缩摘要
export interface Summary {
  content: string;
  replaces_from: string;
  replaces_to: string;
  created_at: number;
}
