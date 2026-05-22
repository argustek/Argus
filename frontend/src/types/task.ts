export interface GlobalTask {
  id: string
  description: string
  role: 'PM' | 'SE' | 'AP' | 'USR'
  status: 'pending' | 'doing' | 'done' | 'failed'
  progress?: string
  progressPercent?: number
  messageId?: string
  parentId?: string
  createdAt: Date
  updatedAt: Date
  completedAt?: Date
  metadata?: Record<string, any>
}

export type TaskStatus = GlobalTask['status']
export type TaskRole = GlobalTask['role']

export const TASK_STATUS_CONFIG = {
  pending: { icon: '⏳', color: '#8b5cf6', label: '等待中' },
  doing: { icon: '🔄', color: '#3b82f6', label: '进行中' },
  done: { icon: '✅', color: '#22c55e', label: '已完成' },
  failed: { icon: '❌', color: '#ef4444', label: '失败' }
} as const

export const ROLE_CONFIG = {
  PM: { icon: '📋', color: '#8b5cf6', label: '项目经理' },
  SE: { icon: '⚙️', color: '#22c55e', label: '软件工程师' },
  AP: { icon: '🔍', color: '#f59e0b', label: '审核员' },
  USR: { icon: '👤', color: '#3b82f6', label: '用户' }
} as const
