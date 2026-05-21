export interface TaskItem {
  id: string
  text: string
  detail?: string
  status: 'pending' | 'running' | 'done' | 'error' | 'skipped'
  startedAt?: number
  completedAt?: number
  duration?: string
  error?: string
}

export interface TaskList {
  id: string
  role: string
  title: string
  tasks: TaskItem[]
  status: 'pending' | 'running' | 'completed' | 'error' | 'cancelled'
  startedAt: number
  endedAt?: number
}

export interface ShellBlock {
  taskId: string
  type: 'write_file' | 'exec' | 'read_file' | 'list_files'
  command: string
  output: string
  exitCode: number
  duration: string
  status: 'running' | 'done' | 'error' | 'blocked'
  timestamp: number
  extra?: Record<string, string>
}

export interface CodeBlock {
  lang: string
  code: string
  copyable: boolean
}

export interface ResultBlock {
  text: string
  codeBlocks?: CodeBlock[]
  jsonData?: any
}

export interface RichMessage {
  id: string
  role: 'pm' | 'se' | 'ap' | 'sys_c' | 'user'
  content?: string
  timestamp: number
  taskList: TaskList
  shells: ShellBlock[]
  result?: ResultBlock
  _streaming?: boolean
}
