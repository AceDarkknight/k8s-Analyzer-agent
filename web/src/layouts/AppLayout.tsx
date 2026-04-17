import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'

const menuItems = [
  { key: '/', icon: '📊', label: '数据概览' },
  { key: '/tasks', icon: '📋', label: '任务列表' },
]

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const [collapsed, setCollapsed] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = menuItems.find((item) =>
    location.pathname.startsWith(item.key) && item.key !== '/'
      ? true
      : location.pathname === item.key,
  )?.key ?? '/'

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        gridTemplateColumns: collapsed ? '80px 1fr' : '220px 1fr',
        background: '#f5f7fb',
      }}
    >
      <aside
        style={{
          background: '#001529',
          color: '#fff',
          padding: '16px 12px',
          transition: 'all 0.2s ease',
        }}
      >
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          style={{
            width: '100%',
            background: 'transparent',
            border: '1px solid rgba(255,255,255,0.15)',
            color: '#fff',
            borderRadius: 8,
            height: 36,
            cursor: 'pointer',
            marginBottom: 16,
          }}
        >
          {collapsed ? '→' : '←'}
        </button>

        <div
          style={{
            height: 48,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            marginBottom: 24,
          }}
        >
          <span style={{ fontSize: 24 }}>☸️</span>
          {!collapsed && (
            <strong
              style={{ color: '#fff', marginLeft: 8, whiteSpace: 'nowrap' }}
            >
              K8s Analyzer
            </strong>
          )}
        </div>

        <nav style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {menuItems.map((item) => {
            const active = selectedKey === item.key
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => navigate(item.key)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  width: '100%',
                  border: 'none',
                  cursor: 'pointer',
                  borderRadius: 8,
                  padding: collapsed ? '12px 0' : '12px 14px',
                  justifyContent: collapsed ? 'center' : 'flex-start',
                  background: active ? '#1677ff' : 'transparent',
                  color: '#fff',
                }}
              >
                <span aria-hidden="true">{item.icon}</span>
                {!collapsed && <span>{item.label}</span>}
              </button>
            )
          })}
        </nav>
      </aside>

      <div style={{ display: 'grid', gridTemplateRows: '64px 1fr' }}>
        <header
          style={{
            background: '#fff',
            padding: '0 24px',
            borderBottom: '1px solid #f0f0f0',
            display: 'flex',
            alignItems: 'center',
          }}
        >
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600 }}>
            监控面板
          </h1>
        </header>
        <main
          style={{
            margin: 24,
            padding: 24,
            background: '#fff',
            borderRadius: 8,
            minHeight: 280,
          }}
        >
          {children}
        </main>
      </div>
    </div>
  )
}
