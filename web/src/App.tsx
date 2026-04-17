import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { lazy, Suspense } from 'react'
import AppLayout from './layouts/AppLayout'
import PageLoader from './components/PageLoader'

const Dashboard = lazy(() => import('./pages/Dashboard'))
const TaskList = lazy(() => import('./pages/TaskList'))
const TaskDetail = lazy(() => import('./pages/TaskDetail'))

export default function App() {
  return (
    <BrowserRouter>
      <AppLayout>
        <Suspense fallback={<PageLoader />}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/tasks" element={<TaskList />} />
            <Route path="/tasks/:id" element={<TaskDetail />} />
          </Routes>
        </Suspense>
      </AppLayout>
    </BrowserRouter>
  )
}
