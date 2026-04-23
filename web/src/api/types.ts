// API 统一响应格式
export interface ApiResponse<T> {
  code: number;
  message: string;
  data: T;
}

// Token 用量
export interface TokenUsage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

// 单次 LLM 调用记录
export interface LLMCallRecord {
  model_type: string;         // "light" | "power"
  model_name: string;         // 实际使用的模型名称
  source: string;             // "decision" | "report" | "deep_query"
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  duration_ms: number;
  timestamp: string;
}

// 推理步骤（Trace 中的 ReasoningStep）
export interface TraceReasoningStep {
  iteration: number;
  timestamp?: string;
  thought: string;
  decision: string;                // "execute_plan" | "deep_query" | "report"
  deep_query_topic?: string;       // 仅 deep_query 时有值
  tool_calls: TraceToolCall[];
  observation: string;
  duration_ms: number;
  tokens_used: number;
}

// 工具调用（ReasoningStep 内的）
export interface TraceToolCall {
  tool_name: string;
  args: Record<string, unknown>;
  success: boolean;
  output: string;
  duration_ms: number;
  timestamp: string;
  cached: boolean;
}

// 工具执行记录（顶层）
export interface TraceToolExecution {
  tool_name: string;
  args: Record<string, unknown>;
  success: boolean;
  output: string;
  duration_ms: number;
  timestamp: string;
  cached: boolean;
}

// 完整任务追踪（TaskTrace）
export interface TaskTrace {
  task_id: string;
  timestamp: string;
  user_input: string;
  status: 'success' | 'failed';
  total_duration_ms: number;
  token_usage: TokenUsage;
  llm_calls: LLMCallRecord[];
  k8s_info: Record<string, unknown>;
  reasoning_history: TraceReasoningStep[];
  tool_executions: TraceToolExecution[];
  analysis_result: string;
  error: string;
  active_skill_name: string;
}

// 任务索引摘要（TraceIndexRecord）
export interface TaskIndexRecord {
  task_id: string;
  timestamp: string;
  user_input: string;
  status: 'success' | 'failed';
  total_duration_ms: number;
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
}

// 任务列表响应
export interface TaskListData {
  items: TaskIndexRecord[];
  total: number;
  page: number;
  size: number;
}

export interface TaskTrendPoint {
  date: string;
  success: number;
  failed: number;
}

export interface ToolUsageRecord {
  tool_name: string;
  success: number;
  failed: number;
}

export interface TaskStatsData {
  total_tasks: number;
  success_tasks: number;
  failed_tasks: number;
  success_rate: number;
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  average_duration_ms: number;
  trend: TaskTrendPoint[];
  tool_usage: ToolUsageRecord[];
}
