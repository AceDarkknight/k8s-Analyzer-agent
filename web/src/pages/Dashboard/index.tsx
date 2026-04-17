import { lazy, Suspense, useEffect, useState } from 'react'
import { fetchTaskStats } from '../../api'
import type { TaskStatsData } from '../../api/types'

const TrendChart = lazy(() => import('./TrendChart'))

function StatCard({
  title,
  value,
  accent,
}: {
  title: string
  value: string | number
  accent?: string
}) {
  return (
    <div
      style={{
        background: '#fff',
        border: '1px solid #edf0f5',
        borderRadius: 12,
        padding: 20,
        boxShadow: '0 1px 2px rgba(16,24,40,0.04)',
      }}
    >
      <div style={{ color: '#667085', fontSize: 14, marginBottom: 8 }}>{title}</div>
      <div style={{ fontSize: 28, fontWeight: 700, color: accent ?? '#101828' }}>{value}</div>
    </div>
  )
}

export default function Dashboard() {
  const [loading, setLoading] = useState(true)
  const [stats, setStats] = useState<TaskStatsData | null>(null)

  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const res = await fetchTaskStats()
        if (!cancelled) {
          setStats(res.data ?? null)
        }
      } catch {
        if (!cancelled) {
          console.error('加载任务数据失败')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <div style={{ fontSize: 16, color: '#667085' }}>加载中...</div>
      </div>
    )
  }

  const total = stats?.total_tasks ?? 0
  const successCount = stats?.success_tasks ?? 0
  const failCount = stats?.failed_tasks ?? 0
  const successRate = (stats?.success_rate ?? 0).toFixed(1)
  const totalTokens = stats?.total_tokens ?? 0
  const avgDuration = stats?.average_duration_ms ?? 0
  const trendData = stats?.trend ?? []

  return (
    <div>
      <h2 style={{ fontSize: 24, marginBottom: 24 }}>
        数据概览
      </h2>

      {total === 0 ? (
        <div
          style={{
            padding: 40,
            borderRadius: 12,
            background: '#fff',
            border: '1px dashed #d0d5dd',
            color: '#667085',
            textAlign: 'center',
          }}
        >
          暂无任务数据，请先执行诊断任务
        </div>
      ) : (
        <>
          <div
            style={{
              display: 'grid',
              gap: 16,
              gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
            }}
          >
            <StatCard title="总任务数" value={total} />
            <StatCard title="成功率" value={`${successRate}%`} accent="#12b76a" />
            <StatCard title="失败任务" value={failCount} accent={failCount > 0 ? '#f04438' : undefined} />
            <StatCard title="总 Token 用量" value={totalTokens.toLocaleString()} />
            <StatCard title="平均耗时" value={`${avgDuration}ms`} />
            <StatCard title="成功 / 失败" value={`${successCount} / ${failCount}`} />
          </div>

          {trendData.length > 1 && (
            <Suspense
              fallback={
                <div
                  style={{
                    marginTop: 24,
                    background: '#fff',
                    borderRadius: 12,
                    padding: 24,
                    border: '1px solid #edf0f5',
                    color: '#667085',
                  }}
                >
                  图表加载中...
                </div>
              }
            >
              <TrendChart data={trendData} />
            </Suspense>
          )}
        </>
      )}
    </div>
  )
}
