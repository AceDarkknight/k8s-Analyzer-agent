import { beforeEach, describe, expect, it, vi } from 'vitest'

const axiosState = vi.hoisted(() => {
  const get = vi.fn()
  const use = vi.fn()
  return { get, use }
})

vi.mock('axios', () => ({
  default: {
    create: () => ({
      get: axiosState.get,
      interceptors: { response: { use: axiosState.use } },
    }),
  },
}))

import { fetchTaskDetail, fetchTaskStats, fetchTasks } from './index'

beforeEach(() => {
  axiosState.get.mockReset()
})

describe('api wrapper', () => {
  it('fetchTasks passes pagination params', async () => {
    axiosState.get.mockResolvedValueOnce({ data: { code: 0, message: 'ok', data: { items: [], total: 0, page: 1, size: 20 } } })

    await fetchTasks(2, 50)

    expect(axiosState.get).toHaveBeenCalledWith('/tasks', { params: { page: 2, size: 50 } })
  })

  it('fetchTaskDetail hits detail endpoint', async () => {
    axiosState.get.mockResolvedValueOnce({ data: { code: 0, message: 'ok', data: { task_id: 't1' } } })

    await fetchTaskDetail('t1')

    expect(axiosState.get).toHaveBeenCalledWith('/tasks/t1')
  })

  it('fetchTaskStats hits stats endpoint', async () => {
    axiosState.get.mockResolvedValueOnce({ data: { code: 0, message: 'ok', data: { total_tasks: 1 } } })

    await fetchTaskStats()

    expect(axiosState.get).toHaveBeenCalledWith('/stats')
  })
})
