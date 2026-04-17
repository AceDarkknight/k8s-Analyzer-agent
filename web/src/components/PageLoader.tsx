import { Spin } from 'antd'

export default function PageLoader() {
  return (
    <div style={{ textAlign: 'center', padding: 80 }}>
      <Spin size="large" tip="加载中..." />
    </div>
  )
}
