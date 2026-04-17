import axios from 'axios';
import type { ApiResponse, TaskListData, TaskStatsData, TaskTrace } from './types';

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
});

http.interceptors.response.use(
  (res) => res,
  (error) => {
    const msg =
      error.response?.data?.message ?? error.message ?? '请求失败';
    console.error('[API Error]', msg);
    return Promise.reject(error);
  },
);

/** 获取任务列表（分页） */
export async function fetchTasks(
  page = 1,
  size = 20,
): Promise<ApiResponse<TaskListData>> {
  const res = await http.get<ApiResponse<TaskListData>>('/tasks', {
    params: { page, size },
  });
  return res.data;
}

/** 获取任务详情 */
export async function fetchTaskDetail(
  taskId: string,
): Promise<ApiResponse<TaskTrace>> {
  const res = await http.get<ApiResponse<TaskTrace>>(`/tasks/${taskId}`);
  return res.data;
}

/** 获取仪表盘统计数据 */
export async function fetchTaskStats(): Promise<ApiResponse<TaskStatsData>> {
  const res = await http.get<ApiResponse<TaskStatsData>>('/stats');
  return res.data;
}
