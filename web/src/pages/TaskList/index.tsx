import { useEffect, useState, useCallback } from 'react';
import { Table, Tag, Button, Typography, message } from 'antd';
import { EyeOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import { fetchTasks } from '../../api';
import type { TaskIndexRecord } from '../../api/types';

const { Title } = Typography;

const statusColorMap: Record<string, string> = {
  success: 'green',
  failed: 'red',
};

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

export default function TaskList() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<TaskIndexRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const loadData = useCallback(async (p: number, ps: number) => {
    setLoading(true);
    try {
      const res = await fetchTasks(p, ps);
      setData(res.data?.items ?? []);
      setTotal(res.data?.total ?? 0);
    } catch {
      message.error('加载任务列表失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData(page, pageSize);
  }, [page, pageSize, loadData]);

  const columns: ColumnsType<TaskIndexRecord> = [
    {
      title: '时间',
      dataIndex: 'timestamp',
      key: 'timestamp',
      width: 180,
      render: (v: string) => (v ? v.replace('T', ' ').slice(0, 19) : '-'),
    },
    {
      title: '用户输入',
      dataIndex: 'user_input',
      key: 'user_input',
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <Tag color={statusColorMap[v] ?? 'default'}>
          {v === 'success' ? '成功' : v === 'failed' ? '失败' : v}
        </Tag>
      ),
    },
    {
      title: '耗时',
      dataIndex: 'total_duration_ms',
      key: 'duration',
      width: 100,
      render: (v: number) => (v ? formatDuration(v) : '-'),
    },
    {
      title: 'Token',
      dataIndex: 'total_tokens',
      key: 'tokens',
      width: 100,
      render: (v: number) => (v ? formatTokens(v) : '-'),
    },
    {
      title: '操作',
      key: 'action',
      width: 100,
      render: (_: unknown, record: TaskIndexRecord) => (
        <Button
          type="link"
          icon={<EyeOutlined />}
          onClick={() => navigate(`/tasks/${record.task_id}`)}
        >
          详情
        </Button>
      ),
    },
  ];

  return (
    <div>
      <Title level={4} style={{ marginBottom: 24 }}>
        任务列表
      </Title>
      <Table<TaskIndexRecord>
        rowKey="task_id"
        columns={columns}
        dataSource={data}
        loading={loading}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />
    </div>
  );
}
