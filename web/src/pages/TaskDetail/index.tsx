import { lazy, Suspense, useEffect, useState } from 'react';
import {
  useParams,
  useNavigate,
} from 'react-router-dom';
import {
  Typography,
  Spin,
  Result,
  Button,
  Descriptions,
  Tag,
  Card,
  Tabs,
  Timeline,
  Collapse,
  Space,
  Table,
  Tooltip,
  message,
} from 'antd';
import {
  ArrowLeftOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined,
  CopyOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { fetchTaskDetail } from '../../api';
import type { TaskTrace, TraceReasoningStep, LLMCallRecord } from '../../api/types';
import { formatDuration, formatTimestamp } from '../../utils/format';

const MarkdownReport = lazy(() => import('./MarkdownReport'))

const { Title, Paragraph } = Typography;

// 执行阶段配置（对应 prompts.go decision 字段）
const decisionConfig: Record<string, { label: string; color: string; dot: string }> = {
  execute_plan: { label: '执行计划', color: 'blue',   dot: 'blue'    },
  deep_query:   { label: '深度调查', color: 'orange', dot: '#fa8c16' },
  report:       { label: '生成报告', color: 'purple', dot: '#722ed1' },
};

// 执行链 Tab 内容
function TracesTab({ trace }: { trace: TaskTrace }) {
  const steps: TraceReasoningStep[] = trace.reasoning_history ?? [];
  const llmCalls: LLMCallRecord[] = trace.llm_calls ?? [];

  if (steps.length === 0) {
    return (
      <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
        暂无执行链数据
      </div>
    );
  }

  // 按顺序将 decision 类型的 LLM 调用与步骤对应（第 i 个 decision 调用 ↔ 第 i 个步骤）
  const decisionCalls = llmCalls.filter(c => c.source === 'decision');
  const stepModelMap: Record<number, LLMCallRecord> = {};
  steps.forEach((step, idx) => {
    if (idx < decisionCalls.length) {
      stepModelMap[step.iteration] = decisionCalls[idx];
    }
  });

  return (
    <Timeline
      items={steps.map((step) => {
        const cfg = decisionConfig[step.decision] ?? {
          label: step.decision,
          color: 'default',
          dot: 'gray',
        };
        const matchedCall = stepModelMap[step.iteration];

        return {
          color: cfg.dot,
          children: (
            <div key={step.iteration} style={{ marginBottom: 12 }}>

              {/* ── 头部行：轮次 + 阶段 + 模型 + tokens + 耗时 ── */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  flexWrap: 'wrap',
                  gap: 6,
                  marginBottom: 6,
                }}
              >
                <span style={{ fontWeight: 600, color: 'rgba(0,0,0,0.85)' }}>
                  第 {step.iteration} 轮
                </span>

                {/* 决策阶段标签 */}
                <Tag color={cfg.color} style={{ margin: 0 }}>
                  {cfg.label}
                </Tag>

                {/* 模型标签（hover 展示完整名称） */}
                {matchedCall && (
                  <Tooltip title={matchedCall.model_name}>
                    <Tag
                      color={matchedCall.model_type === 'power' ? 'volcano' : 'geekblue'}
                      icon={<ThunderboltOutlined />}
                      style={{ margin: 0 }}
                    >
                      {matchedCall.model_type === 'power' ? 'Power' : 'Light'}
                    </Tag>
                  </Tooltip>
                )}

                {/* 决策调用消耗 tokens */}
                {step.tokens_used > 0 && (
                  <span style={{ fontSize: 12, color: '#8c8c8c' }}>
                    {step.tokens_used.toLocaleString()} tokens
                  </span>
                )}

                {/* 耗时 */}
                {step.duration_ms > 0 && (
                  <span style={{ fontSize: 12, color: '#8c8c8c' }}>
                    · 耗时 {formatDuration(step.duration_ms)}
                  </span>
                )}
              </div>

              {/* 深度调查主题（仅 deep_query 时） */}
              {step.decision === 'deep_query' && step.deep_query_topic && (
                <div style={{ marginBottom: 6 }}>
                  <Tag color="orange">调查主题</Tag>
                  <span style={{ fontSize: 13 }}>{step.deep_query_topic}</span>
                </div>
              )}

              {/* 思考 */}
              {step.thought && (
                <div style={{ marginBottom: 6 }}>
                  <Tag color="processing">思考</Tag>
                  <span style={{ fontSize: 13 }}>{step.thought}</span>
                </div>
              )}

              {/* 工具调用列表（每项默认折叠） */}
              {step.tool_calls && step.tool_calls.length > 0 && (
                <Collapse
                  size="small"
                  style={{ marginTop: 4, marginBottom: 4 }}
                  items={step.tool_calls.map((tc, idx) => ({
                    key: idx,
                    label: (
                      <Space>
                        <Tag color={tc.success ? 'success' : 'error'}>
                          {tc.tool_name}
                        </Tag>
                        <span style={{ color: '#999' }}>
                          {formatDuration(tc.duration_ms)}
                        </span>
                        {tc.cached && <Tag color="default">缓存</Tag>}
                      </Space>
                    ),
                    children: (
                      <div>
                        <div style={{ marginBottom: 8 }}>
                          <strong>参数：</strong>
                          <Paragraph copyable style={{ margin: 0 }}>
                            <pre style={{ margin: 0, fontSize: 12 }}>
                              {JSON.stringify(tc.args, null, 2)}
                            </pre>
                          </Paragraph>
                        </div>
                        {tc.output && (
                          <div>
                            <strong>输出摘要：</strong>
                            <Paragraph
                              copyable
                              ellipsis={{ rows: 5, expandable: true }}
                              style={{ margin: 0 }}
                            >
                              {tc.output}
                            </Paragraph>
                          </div>
                        )}
                      </div>
                    ),
                  }))}
                />
              )}

              {/* 观察 — 默认折叠，label 显示内容预览 */}
              {step.observation && (
                <Collapse
                  size="small"
                  style={{ marginTop: 4 }}
                  items={[
                    {
                      key: 'obs',
                      label: (
                        <Space>
                          <Tag color="default" style={{ margin: 0 }}>观察</Tag>
                          <span style={{ color: '#999', fontSize: 12 }}>
                            {step.observation.slice(0, 80)}
                            {step.observation.length > 80 ? '…' : ''}
                          </span>
                        </Space>
                      ),
                      children: (
                        <Paragraph
                          copyable
                          ellipsis={{ rows: 8, expandable: true }}
                          style={{ margin: 0, fontSize: 12, color: 'rgba(0,0,0,0.65)' }}
                        >
                          {step.observation}
                        </Paragraph>
                      ),
                    },
                  ]}
                />
              )}
            </div>
          ),
        };
      })}
    />
  );
}

// 报告 Tab
function ReportTab({ trace }: { trace: TaskTrace }) {
  if (!trace.analysis_result) {
    return (
      <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
        暂无分析报告
      </div>
    );
  }
  return (
    <div style={{ lineHeight: 1.8 }}>
      <Suspense fallback={<Spin size="small" tip="加载报告中..." />}>
        <MarkdownReport content={trace.analysis_result} />
      </Suspense>
    </div>
  );
}

// LLM 调用 Tab
const sourceLabels: Record<string, string> = {
  decision: '决策',
  report: '报告生成',
  deep_query: '深度调查',
};

const sourceColors: Record<string, string> = {
  decision: 'blue',
  report: 'purple',
  deep_query: 'orange',
};

function LLMCallsTab({ trace }: { trace: TaskTrace }) {
  const calls: LLMCallRecord[] = trace.llm_calls ?? [];

  if (calls.length === 0) {
    return (
      <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
        暂无 LLM 调用记录
      </div>
    );
  }

  const columns = [
    {
      title: '序号',
      key: 'index',
      width: 60,
      render: (_: unknown, __: LLMCallRecord, index: number) => index + 1,
    },
    {
      title: '时间',
      dataIndex: 'timestamp',
      key: 'timestamp',
      width: 180,
      render: (v: string) => formatTimestamp(v),
    },
    {
      title: '模型类型',
      dataIndex: 'model_type',
      key: 'model_type',
      width: 100,
      render: (v: string) => (
        <Tag
          icon={<ThunderboltOutlined />}
          color={v === 'power' ? 'volcano' : 'geekblue'}
        >
          {v === 'power' ? 'Power' : 'Light'}
        </Tag>
      ),
    },
    {
      title: '模型名称',
      dataIndex: 'model_name',
      key: 'model_name',
      render: (v: string) => (
        <Tooltip title={v}>
          <span style={{ fontSize: 12, color: '#555' }}>
            {v.length > 30 ? v.slice(0, 30) + '…' : v}
          </span>
        </Tooltip>
      ),
    },
    {
      title: '调用来源',
      dataIndex: 'source',
      key: 'source',
      width: 110,
      render: (v: string) => (
        <Tag color={sourceColors[v] ?? 'default'}>
          {sourceLabels[v] ?? v}
        </Tag>
      ),
    },
    {
      title: 'Prompt Tokens',
      dataIndex: 'prompt_tokens',
      key: 'prompt_tokens',
      width: 130,
      render: (v: number) => v.toLocaleString(),
    },
    {
      title: 'Completion Tokens',
      dataIndex: 'completion_tokens',
      key: 'completion_tokens',
      width: 150,
      render: (v: number) => v.toLocaleString(),
    },
    {
      title: '总 Tokens',
      dataIndex: 'total_tokens',
      key: 'total_tokens',
      width: 100,
      render: (v: number) => <strong>{v.toLocaleString()}</strong>,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      width: 100,
      render: (v: number) => v > 0 ? formatDuration(v) : '—',
    },
  ];

  // 统计摘要
  const totalTokens = calls.reduce((s, c) => s + c.total_tokens, 0);
  const lightCalls = calls.filter((c) => c.model_type === 'light');
  const powerCalls = calls.filter((c) => c.model_type === 'power');
  const lightTokens = lightCalls.reduce((s, c) => s + c.total_tokens, 0);
  const powerTokens = powerCalls.reduce((s, c) => s + c.total_tokens, 0);

  return (
    <div>
      {/* 统计摘要 */}
      <div
        style={{
          display: 'flex',
          gap: 16,
          marginBottom: 16,
          flexWrap: 'wrap',
        }}
      >
        {[
          { label: '总调用次数', value: calls.length },
          { label: 'Light 调用', value: `${lightCalls.length} 次 / ${lightTokens.toLocaleString()} tokens` },
          { label: 'Power 调用', value: `${powerCalls.length} 次 / ${powerTokens.toLocaleString()} tokens` },
          { label: '累计 Tokens', value: totalTokens.toLocaleString() },
        ].map((item) => (
          <div
            key={item.label}
            style={{
              background: '#f5f7fa',
              borderRadius: 8,
              padding: '8px 16px',
              minWidth: 160,
            }}
          >
            <div style={{ fontSize: 12, color: '#667085', marginBottom: 2 }}>{item.label}</div>
            <div style={{ fontWeight: 600, fontSize: 14 }}>{item.value}</div>
          </div>
        ))}
      </div>

      {/* 明细表格 */}
      <Table<LLMCallRecord>
        dataSource={calls}
        columns={columns}
        rowKey={(_, index) => String(index)}
        size="small"
        pagination={false}
        scroll={{ x: 1000 }}
      />
    </div>
  );
}

// 原始 JSON Tab
function RawTab({ trace }: { trace: TaskTrace }) {
  const json = JSON.stringify(trace, null, 2);
  return (
    <div>
      <Paragraph copyable={{ text: json, icon: <CopyOutlined /> }}>
        <pre
          style={{
            background: '#f5f5f5',
            padding: 16,
            borderRadius: 8,
            overflow: 'auto',
            fontSize: 12,
            maxHeight: 600,
          }}
        >
          {json}
        </pre>
      </Paragraph>
    </div>
  );
}

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [trace, setTrace] = useState<TaskTrace | null>(null);
  const [notFound, setNotFound] = useState(false);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await fetchTaskDetail(id);
        if (!cancelled) {
          if (res.code === 40400) {
            setNotFound(true);
          } else {
            setTrace(res.data);
          }
        }
      } catch {
        if (!cancelled) {
          // axios throws on non-2xx, check if 404
          setNotFound(true);
          message.error('加载任务详情失败');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [id]);

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  if (notFound || !trace) {
    return (
      <Result
        status="404"
        title="任务不存在"
        subTitle="未找到该任务的追踪数据"
        extra={
          <Button type="primary" onClick={() => navigate('/tasks')}>
            返回任务列表
          </Button>
        }
      />
    );
  }

  const tu = trace.token_usage ?? {
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
  };

  return (
    <div>
      <Button
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate('/tasks')}
        style={{ marginBottom: 16 }}
      >
        返回列表
      </Button>

      {/* 基础信息卡片 */}
      <Card style={{ marginBottom: 16 }}>
        <Descriptions
          title={
            <Space>
              <Title level={5} style={{ margin: 0 }}>
                任务 #{trace.task_id?.slice(0, 8)}
              </Title>
              {trace.status === 'success' ? (
                <Tag icon={<CheckCircleOutlined />} color="success">成功</Tag>
              ) : trace.status === 'partial' ? (
                <Tag icon={<ExclamationCircleOutlined />} color="warning">部分完成</Tag>
              ) : (
                <Tag icon={<CloseCircleOutlined />} color="error">失败</Tag>
              )}
              {trace.active_skill_name && (
                <Tag color="processing">{trace.active_skill_name}</Tag>
              )}
            </Space>
          }
          bordered
          column={{ xs: 1, sm: 2, lg: 3 }}
        >
          <Descriptions.Item label="用户输入" span={3}>
            {trace.user_input}
          </Descriptions.Item>
          <Descriptions.Item label="开始时间">
            {formatTimestamp(trace.timestamp)}
          </Descriptions.Item>
          <Descriptions.Item label="总耗时">
            {formatDuration(trace.total_duration_ms)}
          </Descriptions.Item>
          <Descriptions.Item label="Token 总量">
            {tu.total_tokens.toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="Prompt Tokens">
            {tu.prompt_tokens.toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="Completion Tokens">
            {tu.completion_tokens.toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="工具调用次数">
            {(trace.tool_executions ?? []).length}
          </Descriptions.Item>
          <Descriptions.Item label="执行轮次">
            {(trace.reasoning_history ?? []).length} 轮
          </Descriptions.Item>
          <Descriptions.Item label="Light 模型调用">
            {(trace.llm_calls ?? []).filter(c => c.model_type === 'light').length} 次
          </Descriptions.Item>
          <Descriptions.Item label="Power 模型调用">
            {(trace.llm_calls ?? []).filter(c => c.model_type === 'power').length} 次
          </Descriptions.Item>
          {trace.error && (
            <Descriptions.Item label="错误信息" span={3}>
              <Paragraph type="danger" style={{ margin: 0 }}>
                {trace.error}
              </Paragraph>
            </Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      {/* 详情 Tabs */}
      <Card>
        <Tabs
          defaultActiveKey="traces"
          items={[
            {
              key: 'traces',
              label: '执行链',
              children: <TracesTab trace={trace} />,
            },
            {
              key: 'llm',
              label: 'LLM 调用',
              children: <LLMCallsTab trace={trace} />,
            },
            {
              key: 'report',
              label: '最终报告',
              children: <ReportTab trace={trace} />,
            },
            {
              key: 'raw',
              label: '原生 JSON',
              children: <RawTab trace={trace} />,
            },
          ]}
        />
      </Card>
    </div>
  );
}
