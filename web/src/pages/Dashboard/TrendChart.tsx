import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { Card } from 'antd'
import type { TaskTrendPoint } from '../../api/types'

export default function TrendChart({ data }: { data: TaskTrendPoint[] }) {
  if (data.length <= 1) {
    return null
  }

  return (
    <Card title="任务趋势" style={{ marginTop: 24 }}>
      <ResponsiveContainer width="100%" height={300}>
        <BarChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="date" />
          <YAxis allowDecimals={false} />
          <Tooltip />
          <Legend />
          <Bar dataKey="success" name="成功" fill="#52c41a" />
          <Bar dataKey="failed" name="失败" fill="#ff4d4f" />
        </BarChart>
      </ResponsiveContainer>
    </Card>
  )
}
