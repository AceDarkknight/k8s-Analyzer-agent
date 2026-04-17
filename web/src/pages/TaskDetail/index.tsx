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
  message,
} from 'antd';
import {
  ArrowLeftOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  CopyOutlined,
} from '@ant-design/icons';
import { fetchTaskDetail } from '../../api';
import type { TaskTrace, TraceReasoningStep } from '../../api/types';
import { formatDuration, formatTimestamp } from '../../utils/format';

const MarkdownReport = lazy(() => import('./MarkdownReport'))

const { Title, Paragraph } = Typography;

// 执行链 Tab 内容
function TracesTab({ trace }: { trace: TaskTrace }) {
  const steps: TraceReasoningStep[] = trace.reasoning_history ?? [];

  if (steps.length === 0) {
    return (
      <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
        暂无执行链数据
      </div>
    );
  }

  return (
    <Timeline
      items={steps.map((step) => ({
        color: step.tool_calls?.length ? 'blue' : 'gray',
        children: (
          <div key={step.iteration} style={{ marginBottom: 8 }}>
            <div
              style={{
                fontWeight: 600,
                marginBottom: 4,
                color: 'rgba(0,0,0,0.85)',
              }}
            >
              第 {step.iteration} 轮
              {step.duration_ms > 0 && (
                <span style={{ fontWeight: 400, color: '#999', marginLeft: 8 }}>
                  耗时 {formatDuration(step.duration_ms)}
                </span>
              )}
            </div>

            {step.thought && (
              <div style={{ marginBottom: 4 }}>
                <Tag color="processing">思考</Tag>
                <span>{step.thought}</span>
              </div>
            )}

            {step.decision && (
              <div style={{ marginBottom: 4 }}>
                <Tag color="warning">决策</Tag>
                <span>{step.decision}</span>
              </div>
            )}

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
                        <Paragraph
                          copyable
                          style={{ margin: 0 }}
                        >
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

            {step.observation && (
              <div style={{ marginTop: 4 }}>
                <Tag color="default">观察</Tag>
                <span style={{ color: 'rgba(0,0,0,0.65)' }}>{step.observation}</span>
              </div>
            )}
          </div>
        ),
      }))}
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
