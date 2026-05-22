# 全局任务追踪器 - 实施计划与设计文档

> **版本**: v1.1  
> **日期**: 2026-05-21  
> **状态**: 待实施  
> **目标**: 实现类似 Trae/Cursor 的全局任务追踪功能  
> **重大更新**: v1.1 - 新增轻量级操作卡片设计 + 与 Monaco 编辑器/xterm 终端深度集成

---

## 📋 一、项目概述

### 1.1 背景与动机

**当前问题**:
- 三层消息模型（TaskList + Shell + Result）信息冗余
- TaskList 显示的是"空话套话"（分析问题、检查代码等），而非具体任务
- 任务状态散落在对话中，用户需要翻找才能了解进度
- 与 Trae/Cursor 的用户体验差距明显

**解决方案**:
- 实现**全局统一任务栏**，固定在对话框底部
- 所有角色共用同一个任务清单
- 每个任务带角色标记 [PM]/[SE]/[AP]
- 通过 SSE 实时推送更新

### 1.2 设计目标

| 目标 | 描述 | 优先级 |
|------|------|--------|
| ✅ 统一视图 | 所有角色的任务在一个地方显示 | P0 |
| ✅ 固定位置 | 不随对话滚动，始终可见 | P0 |
| ✅ 实时更新 | SSE 推送即时刷新任务状态 | P0 |
| ✅ 角色标记 | 每个任务带 [PM]/[SE]/[AP] 后缀 | P0 |
| ✅ 具体任务 | 禁止空话，只显示具体动作 | P0 |
| ⚙️ 可配置 | 支持折叠、过滤、排序 | P1 |
| 💾 历史记录 | 保留已完成的任务供回顾 | P2 |

### 1.3 参考产品

- **Trae (字节跳动)**: 底部任务栏 + 实时日志
- **Cursor AI**: 左侧任务面板 + 主编辑区
- **GitHub Copilot Chat**: 内联任务展示

---

## 🎨 二、UI/UX 设计

### 2.1 整体布局

```
┌─ Argus Desktop ───────────────────────────────────┐
│                                                    │
│ ┌─ 💬 对话区域 (可滚动) ───────────────────────┐   │
│ │                                              │   │
│ │  USR: hello pls write a hello world...       │   │
│ │                                              │   │
│ │  PM: @SE 请创建 hello.go...                  │   │
│ │                                              │   │
│ │  SE: (Shell 输出)                            │   │
│ │      ▶ exec: go run hello.go                │   │
│ │      Hello World                             │   │
│ │                                              │   │
│ │  PM: @AP 任务已验证...                        │   │
│ │                                              │   │
│ └──────────────────────────────────────────────┘   │
│                                                    │
│ ═══════════════════════════════════════════════    │
│                                                    │
│ ┌─ 📋 全局任务追踪器 ─────────────────────────┐   │
│ │                                             │   │
│ │  ✓ 写入 hello.go 文件            [PM]  ✓     │   │
│ │  ▶ 运行程序验证输出              [SE]  ...   │   │
│ │  ○ 最终质量审批                  [AP]        │   │
│ │                                             │   │
│ └─────────────────────────────────────────────┘   │
│                                                    │
│ ┌─ 输入框 ───────────────────────────────────┐     │
│ │  发送消息给 AI...                     [发送]│     │
│ └──────────────────────────────────────────┘     │
└────────────────────────────────────────────────────┘
```

### 2.2 任务栏组件结构

#### **2.2.1 标题栏**

```
┌─────────────────────────────────────────────────┐
│ 📋  任务追踪   [3]   ⚙️ 设置   ▼ 折叠   − 最小化   × 关闭  │
└─────────────────────────────────────────────────┘
```

**元素说明**:
- `📋` 图标 + "任务追踪" 标题
- `[3]` 徽章：当前活跃任务数量
- `⚙️ 设置`: 配置选项（过滤、排序、显示设置）
- `▼ 折叠`: 收起/展开任务列表
- `− 最小化`: 缩小为单行图标模式
- `× 关闭`: 隐藏任务栏（可通过快捷键或菜单重新打开）

#### **2.2.2 任务项**

```
┌─────────────────────────────────────────────────┐
│  ✓  写入 hello.go 文件              [PM]  ✓     │
│  ▶  运行程序验证输出              [SE]  ...     │
│  ○  最终质量审批                    [AP]        │
└─────────────────────────────────────────────────┘
```

**元素说明**:

| 元素 | 说明 | 示例 |
|------|------|------|
| 状态图标 | ○ pending / ▶ doing / ✓ done / ✗ failed | `▶` |
| 任务描述 | 具体要做什么（禁止空话） | `写入 hello.go 文件` |
| 角色标签 | 责任归属 | `[PM]` / `[SE]` / `[AP]` |
| 进度信息 | 可选的实时进度 | `...` / `50%` / `go run` |

#### **2.2.3 三种显示状态**

**状态 1: 完全展开（默认）**
```
高度: ~120px
显示: 标题栏 + 完整任务列表（最多 10 个）
```

**状态 2: 折叠**
```
高度: ~36px
显示: 仅标题栏（带任务数量徽章）
点击展开按钮恢复
```

**状态 3: 最小化**
```
高度: ~28px
显示: 单行图标模式
  ✓  ▶  ○     [展开]
仅显示各状态的任务数量图标
```

### 2.3 视觉样式规范

#### **颜色系统**

```css
/* 状态颜色 */
--status-pending: #999999;      /* 灰色 */
--status-doing: #1890ff;        /* 蓝色 - 带脉冲动画 */
--status-done: #52c41a;         /* 绿色 */
--status-failed: #ff4d4f;       /* 红色 */

/* 角色标签颜色 */
--role-pm: #1890ff;             /* 蓝色 */
--role-se: #fa8c16;             /* 橙色 */
--role-ap: #722ed1;             /* 紫色 */
--role-usr: #13c2c2;            /* 青色 */

/* 背景色 */
--bg-primary: #ffffff;
--bg-hover: #f5f5f5;
--bg-selected: #e6f7ff;

/* 边框色 */
--border-color: #d9d9d9;
--border-active: #1890ff;
```

#### **动画效果**

```css
/* 进行中任务的脉冲效果 */
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.task-item.doing {
  animation: pulse 2s infinite;
}

/* 新任务添加时的滑入动画 */
@keyframes slideIn {
  from {
    transform: translateX(-20px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

.task-item.new {
  animation: slideIn 0.3s ease-out;
}

/* 任务完成时的淡出效果 */
@keyframes fadeOut {
  from { opacity: 1; }
  to { opacity: 0.5; }
}

.task-item.done {
  animation: fadeOut 0.5s ease-out;
}
```

---

## 🎨 二点五、对话活动区详细设计（轻量级操作卡片）

### **核心设计理念：复用现有专业工具，不重复造轮子**

**关键洞察**: Argus 已有两个强大的界面组件：
1. **Monaco 编辑器** (`EditorArea.vue`) - VS Code 同款代码编辑器
2. **xterm.js 终端** (`XtermTerminal.vue`) - 专业终端仿真器

**错误做法** (❌ 不要这样做):
- 在消息内再做一个假的终端模拟器（静态文本，不可交互）
- 在消息内做代码预览块（只读，无语法高亮）
- 与右侧的专业工具功能重复

**正确做法** (✅ 采用方案):
- 对话区只显示**操作摘要 + 快捷按钮**
- 点击文件 → 跳转到 Monaco 编辑器打开
- 点击命令 → 跳转到 xterm 终端执行
- 用户可以在真实工具中继续交互和编辑

### **2.5.1 现有界面布局**

```
┌─ TopBar (顶部状态栏) ─────────────────────────────────┐
│                                                        │
├────────────┬───────────────────────────────────────────┤
│            │  ┌─ EditorWindow (Monaco 编辑器) ─────┐   │
│            │  │  多标签页 | 语法高亮 | AI 输入框    │   │
│  ChatPanel │  └────────────────────────────────────┘   │
│  (对话区)   │  ═══════════════════════════════════     │
│            │  ┌─ TerminalWindow (xterm.js 终端) ────┐   │
│            │  │  真终端仿真 | 命令历史 | 右键菜单    │   │
│            │  └────────────────────────────────────┘   │
└────────────┴───────────────────────────────────────────┘
                    ↑ 可拖拽调整比例
```

### **2.5.2 SE 消息重新设计：轻量级操作卡片**

#### **视觉效果对比**

**旧方案（消息内模拟终端）- 已废弃**:
```
┌─ ⚙ SE ──────────────────────────────┐
│ 2 个操作                              │
│ ┌─ 终端 (假) ─────────────────────┐   │
│ │ $ go run hello.go               │   │
│ │ Hello World                     │   │
│ │ ✅ Exit: 0                      │   │
│ └─────────────────────────────────┘   │
│ ✅ 任务完成                           │
└───────────────────────────────────────┘
问题: ❌ 假终端不能交互 | ❌ 占用空间大 | ❌ 与真终端重复
```

**新方案（轻量级卡片 + 跳转真实工具）- ✅ 采用**:
```
┌─ ⚙ SE ──────────────────────────────┐
│ 2 个操作                              │
│                                       │
│ 📄 创建                               │
│    hello.go                    ✅ 已创建  128B
│                                       │
│ ▶ 执行                               │
│    go run hello.go           [▶ 运行]  ✅ 成功  0.05s
│                                       │
│ ✅ 全部完成 (2/2)                     │
│ [📂 编辑器查看] [🔄 查看变更]          │
└───────────────────────────────────────┘
优势: ✅ 简洁 | ✅ 可跳转编辑器/终端 | ✅ 复用专业工具
```

#### **SERichMessage.vue 完整组件结构**

```vue
<template>
  <div class="message-item se" :id="message.id">
    <div class="avatar">⚙️</div>
    <div class="content">
      
      <!-- 头部 -->
      <div class="header">
        <span class="role-tag">SE</span>
        <span class="action-count">{{ actions.length }} 个操作</span>
        
        <div class="quick-actions">
          <button @click="openAllInEditor" title="全部在编辑器中打开">📂</button>
          <button @click="runAllInTerminal" title="全部在终端中运行">💻</button>
          <button @click="toggleExpand">{{ isExpanded ? '▲' : '▼' }}</button>
        </div>
      </div>

      <!-- 操作列表 (展开时显示) -->
      <div class="action-list" v-show="isExpanded">
        <div 
          v-for="(action, index) in actions" 
          :key="index"
          class="action-item"
          :class="[action.type, action.status]"
        >
          
          <!-- write_file / edit_file 类型 -->
          <template v-if="action.type === 'write_file' || action.type === 'edit_file'">
            <div class="file-action">
              <span class="icon">📄</span>
              <span class="action-label">{{ action.type === 'write_file' ? '创建' : '修改' }}</span>
              
              <!-- 可点击的文件名 -->
              <code 
                class="filename" 
                @click="$emit('open-file-in-editor', action.path)"
                title="点击在 Monaco 编辑器中打开"
              >
                {{ action.path }}
              </code>
              
              <span class="status-badge" :class="action.status">
                {{ getFileStatusText(action.status) }}
              </span>
              
              <span class="file-size">{{ calculateSize(action.content) }} bytes</span>
            </div>
            
            <!-- 快速预览 (可选，默认折叠，双击可打开编辑器) -->
            <div 
              class="quick-preview" 
              v-if="showPreviews.includes(index)"
              @dblclick="$emit('open-file-in-editor', action.path)"
            >
              <pre><code>{{ action.content }}</code></pre>
              <div class="preview-hint">💡 双击在编辑器中打开</div>
            </div>
          </template>

          <!-- exec 类型 -->
          <template v-else-if="action.type === 'exec'">
            <div class="exec-action">
              <span class="icon">▶</span>
              <span class="action-label">执行</span>
              
              <!-- 可点击的命令 -->
              <code 
                class="command" 
                @click="$emit('run-in-terminal', action.command)"
                title="点击在 xterm 终端中执行"
              >
                {{ action.command }}
              </code>
              
              <!-- 运行按钮 -->
              <button 
                class="run-btn"
                @click="$emit('run-in-terminal', action.command)"
                title="在终端中运行"
              >▶ 运行</button>
              
              <span v-if="action.duration" class="duration">⏱️ {{ action.duration }}</span>
              
              <span class="status-badge" :class="action.status">
                {{ getExecStatusText(action.status, action.exitCode) }}
              </span>
            </div>
            
            <!-- 输出摘要 (如果已执行过，默认折叠) -->
            <div v-if="action.output && showOutputs.includes(index)" class="output-summary">
              <div class="output-header">
                <span>输出预览:</span>
                <button @click="$emit('view-output-in-terminal', action)">
                  在终端中查看完整输出 →
                </button>
              </div>
              <pre class="output-text">{{ truncateOutput(action.output, 10) }}</pre>
            </div>
          </template>

          <!-- read_file 类型 -->
          <template v-else-if="action.type === 'read_file'">
            <div class="read-action">
              <span class="icon">👁️</span>
              <span class="action-label">读取</span>
              <code 
                class="filename" 
                @click="$emit('open-file-in-editor', action.path)"
                title="点击在编辑器中打开"
              >
                {{ action.path }}
              </code>
              <span class="status-badge success">✅ 已读取</span>
            </div>
          </template>

        </div>
      </div>

      <!-- 结果摘要 -->
      <div class="result-summary" :class="overallStatus">
        <span class="result-icon">{{ getResultIcon(overallStatus) }}</span>
        <span class="result-text">{{ resultText }}</span>
        
        <!-- 操作链接 -->
        <div class="result-actions">
          <button 
            v-if="overallStatus === 'success'" 
            @click="$emit('open-all-files')"
            class="action-link"
          >📂 在编辑器中查看所有文件</button>
          
          <button 
            v-if="overallStatus === 'success' && hasModifications" 
            @click="$emit('view-diff')"
            class="action-link"
          >🔄 查看变更差异</button>
          
          <button 
            v-if="overallStatus === 'failed'" 
            @click="$emit('retry-failed')"
            class="action-link warning"
          >🔄 重试失败的操作</button>
        </div>
      </div>

      <div class="timestamp">{{ formatTime(message.timestamp) }}</div>
      
    </div>
  </div>
</template>
```

#### **事件定义**

```typescript
// SERichMessage.vue emit 的事件
const emit = defineEmits([
  'open-file-in-editor',    // 打开文件到 Monaco 编辑器
  'run-in-terminal',         // 在 xterm 终端中执行命令
  'view-output-in-terminal', // 在终端中查看完整输出
  'open-all-files',          // 打开所有相关文件
  'view-diff',               // 查看变更差异
  'retry-failed',            // 重试失败操作
])
```

### **2.5.3 各角色消息详细设计**

#### **USR (用户) - 纯文本消息**
```vue
<template>
  <div class="message-item user">
    <div class="avatar">👤</div>
    <div class="content">
      <div class="role-tag">USR</div>
      <div class="text">{{ message.content }}</div>
      <div class="timestamp">{{ formatTime(message.timestamp) }}</div>
    </div>
  </div>
</template>
```
**样式**: 渐变蓝色背景 `#e6f7ff → #f0f9ff`，左侧蓝色边框 `4px solid #1890ff`

---

#### **PM (项目经理) - 纯文本 + 引用高亮**
```vue
<template>
  <div class="message-item pm">
    <div class="avatar">📋</div>
    <div class="content">
      <div class="header">
        <span class="role-tag">PM</span>
        <span class="task-badge" v-if="relatedTask">
          📋 {{ relatedTask.description }}
          <span class="status-dot" :class="relatedTask.status"></span>
        </span>
      </div>
      
      <div class="text" v-html="formatContent(message.content)"></div>
      
      <!-- @提及高亮 -->
      <div class="mentions" v-if="mentions.length">
        <span 
          v-for="mention in mentions" 
          :key="mention.role"
          class="mention-tag"
          :class="mention.role"
        >@{{ mention.role }} {{ mention.text }}</span>
      </div>
      
      <div class="timestamp">{{ formatTime(message.timestamp) }}</div>
    </div>
  </div>
</template>
```
**特性**: 显示关联任务状态指示灯；`@SE` / `@AP` 提及高亮显示

---

#### **AP (审核员) - 审核报告卡片**
```vue
<template>
  <div class="message-item ap">
    <div class="avatar">🔍</div>
    <div class="content">
      <div class="header">
        <span class="role-tag">AP</span>
        <span class="review-status" :class="reviewResult">
          {{ reviewResult === 'approved' ? '✅ 通过' : '❌ 驳回' }}
        </span>
      </div>

      <p class="conclusion">{{ message.content }}</p>
      
      <!-- 审核详情 (可折叠) -->
      <div class="review-details" v-if="showDetails">
        <div class="detail-section">
          <h4>📋 代码质量</h4>
          <div class="score-bar">
            <div class="fill" :style="{ width: codeQualityScore + '%' }"></div>
          </div>
          <span>{{ codeQualityScore }}/100</span>
        </div>

        <div class="detail-section">
          <h4>✅ 测试验证</h4>
          <ul>
            <li v-for="test in testResults" :key="test.name">
              {{ test.name }}: 
              <span :class="test.passed ? 'pass' : 'fail'">
                {{ test.passed ? '通过' : '未通过' }}
              </span>
            </li>
          </ul>
        </div>
      </div>

      <button @click="showDetails = !showDetails">
        {{ showDetails ? '隐藏详情' : '查看审核报告' }}
      </button>
      
      <div class="timestamp">{{ formatTime(message.timestamp) }}</div>
    </div>
  </div>
</template>
```

### **2.5.4 核心样式规范**

```scss
// SE 操作项样式
.action-item {
  padding: 10px 12px;
  margin: 6px 0;
  background: #fafafa;
  border-radius: 6px;
  border-left: 3px solid transparent;
  transition: all 0.2s;

  &:hover { background: #f5f5f5; }

  &.write_file,
  &.edit_file { border-left-color: #1890ff; }
  &.exec { border-left-color: #52c41a; }
  &.read_file { border-left-color: #722ed1; }
}

// 文件名/命令 - 可点击样式
code.filename,
code.command {
  font-family: 'Consolas', monospace;
  font-size: 13px;
  padding: 3px 8px;
  border-radius: 4px;
  cursor: pointer;
  position: relative;
  transition: all 0.2s;

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 2px 8px rgba(24, 144, 255, 0.2);

    &::after {  // 提示气泡
      content: attr(title);
      position: absolute;
      bottom: 100%;
      left: 50%;
      transform: translateX(-50%);
      background: rgba(0, 0, 0, 0.85);
      color: white;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 11px;
      white-space: nowrap;
      font-family: system-ui;
    }
  }
}

// 文件操作
code.filename {
  background: #e6f7ff;
  color: #0969da;
  
  &:hover { background: #bae7ff; border: 1px solid #1890ff; }
}

// 命令操作
code.command {
  background: #f6ffed;
  color: #389e0d;
  border: 1px solid #b7eb8f;
  
  &:hover { background: #d9f7be; border-color: #52c41a; }
}

// 运行按钮
.run-btn {
  padding: 4px 12px;
  background: #52c41a;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-weight: 500;
  
  &:hover {
    background: #73d13d;
    transform: scale(1.05);
  }
}

// 状态徽章
.status-badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 10px;
  margin-left: auto;

  &.success { background: #f6ffed; color: #52c41a; }
  &.error { background: #fff2f0; color: #ff4d4f; }
  &.running { 
    background: #e6f7ff; color: #1890ff; 
    animation: pulse 1.5s infinite; 
  }
}

// 结果摘要
.result-summary {
  margin-top: 14px;
  padding: 12px 16px;
  border-radius: 8px;

  &.success {
    background: linear-gradient(135deg, #f0fdf4 0%, #dcfce7 100%);
    border: 1px solid #86efac;
  }

  &.failed {
    background: linear-gradient(135deg, #fef2f2 0%, #fecaca 100%);
    border: 1px solid #fca5a5;
  }

  .result-actions {
    margin-top: 10px;
    
    .action-link {
      background: none;
      border: none;
      text-decoration: underline;
      cursor: pointer;
      opacity: 0.8;
      
      &:hover { opacity: 1; }
      &.warning { color: #fa8c16; }
      &.error { color: #ff4d4f; }
    }
  }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
```

---

## 🔧 三、技术架构

### 3.1 数据模型

#### **3.1.1 任务数据结构 (TypeScript)**

```typescript
interface GlobalTask {
  id: string;                  // 唯一标识符 (UUID)
  description: string;         // 任务描述（具体动作）
  role: 'PM' | 'SE' | 'AP' | 'USR';  // 角色标记
  status: 'pending' | 'doing' | 'done' | 'failed';
  
  progress?: string;           // 可选进度信息
  progressPercent?: number;    // 可选百分比 0-100
  
  messageId?: string;          // 关联的消息 ID（用于跳转）
  parentId?: string;           // 父任务 ID（支持子任务）
  
  createdAt: Date;             // 创建时间
  updatedAt: Date;             // 最后更新时间
  completedAt?: Date;          // 完成时间
  
  metadata?: Record<string, any>;  // 扩展元数据
}
```

#### **3.1.2 Go 后端数据结构**

```go
type GlobalTask struct {
    ID          string                 `json:"id"`
    Description string                 `json:"description"`
    Role        string                 `json:"role"`  // "PM", "SE", "AP", "USR"
    Status      string                 `json:"status"`  // "pending", "doing", "done", "failed"
    
    Progress    string                 `json:"progress,omitempty"`
    ProgressPercent int                `json:"progressPercent,omitempty"`
    
    MessageID   string                 `json:"messageId,omitempty"`
    ParentID    string                 `json:"parentId,omitempty"`
    
    CreatedAt   time.Time              `json:"createdAt"`
    UpdatedAt   time.Time              `json:"updatedAt"`
    CompletedAt *time.Time             `json:"completedAt,omitempty"`
}
```

### 3.2 后端实现

#### **3.2.1 SSE 事件类型扩展**

```go
// 在现有 SSE 事件基础上新增

type TaskEventType string

const (
    TaskAdded    TaskEventType = "task_added"
    TaskUpdated  TaskEventType = "task_updated"
    TaskCompleted TaskEventType = "task_completed"
    TaskFailed   TaskEventType = "task_failed"
)

type TaskEvent struct {
    Type        TaskEventType `json:"type"`        // 事件类型
    Task        GlobalTask    `json:"task"`        // 完整任务对象
    SessionID   string        `json:"sessionId"`   // 会话 ID
    Timestamp   time.Time     `json:"timestamp"`   // 时间戳
}

// 推送函数
func (m *Manager) PushTaskUpdate(event TaskEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }
    
    return m.sseServer.Broadcast("task_update", string(data))
}
```

#### **3.2.2 任务管理器**

```go
type TaskManager struct {
    tasks      map[string]*GlobalTask  // 任务存储
    mu         sync.RWMutex           // 读写锁
    sseServer  *SSEServer             // SSE 推送服务
    manager    *Manager               // 引用 Manager
}

// 创建新任务
func (tm *TaskManager) CreateTask(description, role string) (*GlobalTask, error) {
    task := &GlobalTask{
        ID:          generateUUID(),
        Description: description,
        Role:        role,
        Status:      "pending",
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    
    tm.mu.Lock()
    tm.tasks[task.ID] = task
    tm.mu.Unlock()
    
    // 推送事件
    tm.PushTaskUpdate(TaskAdded, task)
    
    return task, nil
}

// 更新任务状态
func (tm *TaskManager) UpdateTaskStatus(taskId, status string, progress ...string) error {
    tm.mu.Lock()
    defer tm.mu.Unlock()
    
    task, exists := tm.tasks[taskId]
    if !exists {
        return fmt.Errorf("task not found: %s", taskId)
    }
    
    task.Status = status
    task.UpdatedAt = time.Now()
    
    if len(progress) > 0 {
        task.Progress = progress[0]
    }
    
    if status == "done" || status == "failed" {
        now := time.Now()
        task.CompletedAt = &now
    }
    
    // 推送事件
    eventType := TaskUpdated
    if status == "done" {
        eventType = TaskCompleted
    } else if status == "failed" {
        eventType = TaskFailed
    }
    
    go tm.PushTaskUpdate(eventType, task)
    
    return nil
}
```

#### **3.2.3 API 端点**

```go
// 获取所有任务
func (a *App) GetTasks(ctx context.Context) ([]GlobalTask, error) {
    return a.taskManager.GetAllTasks(), nil
}

// 手动添加任务（可选，用于用户自定义）
func (a *App) AddTask(ctx context.Context, description, role string) (*GlobalTask, error) {
    return a.taskManager.CreateTask(description, role)
}

// 更新任务（可选）
func (a *App) UpdateTask(ctx context.Context, taskId string, updates map[string]interface{}) error {
    return a.taskManager.UpdateTask(taskId, updates)
}

// 注册路由
func (a *App) setupTaskRoutes() {
    a.app.GET("/api/tasks", a.GetTasks)
    a.app.POST("/api/tasks", a.AddTask)
    a.app.PUT("/api/tasks/:id", a.UpdateTask)
}
```

### 3.3 前端实现

#### **3.3.1 Pinia Store (状态管理)**

```typescript
// stores/taskStore.ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { GlobalTask } from '@/types'

export const useTaskStore = defineStore('tasks', () => {
  const tasks = ref<GlobalTask[]>([])
  const isCollapsed = ref(false)
  const isMinimized = ref(false)
  const isVisible = ref(true)
  
  const activeTasks = computed(() => 
    tasks.value.filter(t => t.status !== 'done' && t.status !== 'failed')
  )
  
  const completedTasks = computed(() =>
    tasks.value.filter(t => t.status === 'done' || t.status === 'failed')
  )
  
  const taskStats = computed(() => ({
    total: tasks.value.length,
    active: activeTasks.value.length,
    completed: completedTasks.value.length,
    doing: tasks.value.filter(t => t.status === 'doing').length,
  }))
  
  function addTask(task: Omit<GlobalTask, 'id' | 'createdAt' | 'updatedAt'>) {
    const newTask: GlobalTask = {
      ...task,
      id: generateId(),
      createdAt: new Date(),
      updatedAt: new Date(),
    }
    tasks.value.unshift(newTask)  // 新任务置顶
  }
  
  function updateTask(id: string, updates: Partial<GlobalTask>) {
    const index = tasks.value.findIndex(t => t.id === id)
    if (index !== -1) {
      tasks.value[index] = {
        ...tasks.value[index],
        ...updates,
        updatedAt: new Date(),
      }
    }
  }
  
  function removeTask(id: string) {
    tasks.value = tasks.value.filter(t => t.id !== id)
  }
  
  function clearCompleted() {
    tasks.value = tasks.value.filter(t => 
      t.status !== 'done' && t.status !== 'failed'
    )
  }
  
  // 监听 SSE 事件
  function listenToSSE() {
    sse.on('task_update', (event: TaskEvent) => {
      switch (event.Type) {
        case 'task_added':
          addTask(event.Task)
          break
        case 'task_updated':
        case 'task_completed':
        case 'task_failed':
          updateTask(event.Task.ID, event.Task)
          break
      }
    })
  }
  
  return {
    tasks,
    isCollapsed,
    isMinimized,
    isVisible,
    activeTasks,
    completedTasks,
    taskStats,
    addTask,
    updateTask,
    removeTask,
    clearCompleted,
    listenToSSE,
    toggleCollapse: () => isCollapsed.value = !isCollapsed.value,
    toggleMinimize: () => isMinimized.value = !isMinimized.value,
    toggleVisible: () => isVisible.value = !isVisible.value,
  }
})
```

#### **3.3.2 主组件 (GlobalTaskBar.vue)**

```vue
<template>
  <div 
    v-show="isVisible"
    class="global-task-bar"
    :class="{ collapsed: isCollapsed, minimized: isMinimized }"
  >
    <!-- 标题栏 -->
    <div class="task-bar-header">
      <div class="header-left">
        <span class="icon">📋</span>
        <span class="title">任务追踪</span>
        <span class="badge">{{ taskStats.active }}</span>
      </div>
      
      <div class="header-actions">
        <button 
          v-if="!isMinimized"
          @click="showSettings = true"
          title="设置"
        >⚙️</button>
        
        <button 
          @click="toggleCollapse"
          :title="isCollapsed ? '展开' : '折叠'"
        >{{ isCollapsed ? '▲' : '▼' }}</button>
        
        <button 
          @click="toggleMinimize"
          :title="isMinimized ? '还原' : '最小化'"
        >{{ isMinimized ? '□' : '−' }}</button>
        
        <button 
          @click="toggleVisible"
          title="关闭"
        >×</button>
      </div>
    </div>

    <!-- 任务列表 -->
    <div v-if="!isCollapsed && !isMinimized" class="task-list">
      <!-- 活跃任务 -->
      <div 
        v-for="task in activeTasks" 
        :key="task.id"
        class="task-item"
        :class="[task.status, `role-${task.role.toLowerCase()}`]"
        @click="$emit('task-click', task)"
      >
        <span class="status-icon">{{ getStatusIcon(task.status) }}</span>
        <span class="description">{{ task.description }}</span>
        <span class="role-tag" :class="task.role">[{{ task.role }}]</span>
        <span v-if="task.progress" class="progress">{{ task.progress }}</span>
      </div>

      <!-- 已完成任务（可折叠） -->
      <div v-if="completedTasks.length > 0" class="completed-section">
        <div class="section-header" @click="showCompleted = !showCompleted">
          <span>{{ showCompleted ? '▼' : '▶' }} 已完成 ({{ completedTasks.length }})</span>
        </div>
        
        <div v-show="showCompleted" class="completed-list">
          <div 
            v-for="task in completedTasks" 
            :key="task.id"
            class="task-item done"
          >
            <span class="status-icon">✓</span>
            <span class="description">{{ task.description }}</span>
            <span class="role-tag" :class="task.role">[{{ task.role }}]</span>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-if="tasks.length === 0" class="empty-state">
        暂无任务
      </div>
    </div>

    <!-- 最小化模式 -->
    <div v-if="isMinimized" class="minimized-view">
      <span 
        v-for="(count, status) in statusCounts" 
        :key="status"
        class="mini-icon"
        :class="status"
        :title="`${status}: ${count}`"
      >{{ getStatusIcon(status) }} {{ count }}</span>
      
      <button @click="toggleMinimize">[展开]</button>
    </div>

    <!-- 设置面板 -->
    <SettingsPanel 
      v-if="showSettings"
      @close="showSettings = false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useTaskStore } from '@/stores/taskStore'
import SettingsPanel from './SettingsPanel.vue'

const taskStore = useTaskStore()

const { 
  tasks, 
  isCollapsed, 
  isMinimized, 
  isVisible,
  activeTasks,
  completedTasks,
  taskStats,
  toggleCollapse,
  toggleMinimize,
  toggleVisible,
} = taskStore

const showSettings = ref(false)
const showCompleted = ref(false)

const statusCounts = computed(() => ({
  doing: tasks.value.filter(t => t.status === 'doing').length,
  done: tasks.value.filter(t => t.status === 'done').length,
  pending: tasks.value.filter(t => t.status === 'pending').length,
}))

function getStatusIcon(status: string): string {
  const icons: Record<string, string> = {
    pending: '○',
    doing: '▶',
    done: '✓',
    failed: '✗',
  }
  return icons[status] || '?'
}
</script>

<style scoped lang="scss">
.global-task-bar {
  border-top: 1px solid var(--border-color);
  background: var(--bg-primary);
  transition: all 0.3s ease;

  &.collapsed {
    .task-list {
      display: none;
    }
  }

  &.minimized {
    .task-list {
      display: none;
    }

    .minimized-view {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 12px;
      padding: 8px 16px;
    }
  }
}

.task-bar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  background: #fafafa;
  border-bottom: 1px solid var(--border-color);

  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;

    .icon { font-size: 18px; }
    .title { font-weight: 600; font-size: 14px; }
    
    .badge {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 20px;
      height: 20px;
      padding: 0 6px;
      border-radius: 10px;
      background: #1890ff;
      color: white;
      font-size: 12px;
      font-weight: 500;
    }
  }

  .header-actions {
    display: flex;
    gap: 8px;

    button {
      padding: 4px 8px;
      border: none;
      background: transparent;
      cursor: pointer;
      border-radius: 4px;
      font-size: 14px;

      &:hover {
        background: rgba(0, 0, 0, 0.04);
      }
    }
  }
}

.task-list {
  max-height: 200px;
  overflow-y: auto;
  padding: 8px 0;

  .task-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: var(--bg-hover);
    }

    .status-icon {
      width: 20px;
      text-align: center;
      font-size: 14px;
      flex-shrink: 0;
    }

    .description {
      flex: 1;
      font-size: 14px;
      color: #333;
    }

    .role-tag {
      font-size: 12px;
      padding: 2px 6px;
      border-radius: 4px;
      font-weight: 500;
      flex-shrink: 0;

      &.PM { background: #e6f7ff; color: #1890ff; }
      &.SE { background: #fff7e6; color: #fa8c16; }
      &.AP { background: #f9f0ff; color: #722ed1; }
      &.USR { background: #e6fffb; color: #13c2c2; }
    }

    .progress {
      font-size: 12px;
      color: #999;
      max-width: 150px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    &.doing {
      animation: pulse 2s infinite;
      background: #fffbe6;
    }

    &.done {
      opacity: 0.7;
      text-decoration: line-through;
    }

    &.failed {
      background: #fff2f0;
      color: #ff4d4f;
    }
  }
}

.completed-section {
  margin-top: 8px;
  border-top: 1px solid #f0f0f0;

  .section-header {
    padding: 8px 16px;
    cursor: pointer;
    font-size: 13px;
    color: #666;
    user-select: none;

    &:hover {
      background: var(--bg-hover);
    }
  }

  .completed-list {
    .task-item {
      padding: 8px 16px;
      font-size: 13px;
    }
  }
}

.empty-state {
  padding: 20px;
  text-align: center;
  color: #999;
  font-size: 14px;
}

.mini-icon {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;

  &.doing { color: #1890ff; }
  &.done { color: #52c41a; }
  &.pending { color: #999; }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}
</style>
```

#### **3.3.3 设置组件 (SettingsPanel.vue)**

```vue
<template>
  <div class="settings-panel">
    <h3>任务栏设置</h3>
    
    <div class="setting-item">
      <label>
        <input type="checkbox" v-model="settings.showRoleFilter" />
        显示角色过滤器
      </label>
    </div>

    <div class="setting-item">
      <label>最大显示任务数:</label>
      <input type="number" v-model.number="settings.maxTasks" min="5" max="50" />
    </div>

    <div class="setting-item">
      <label>
        <input type="checkbox" v-model="settings.autoClearCompleted" />
        自动清除已完成任务
      </label>
    </div>

    <div class="setting-item">
      <label>默认状态:</label>
      <select v-model="settings.defaultState">
        <option value="expanded">展开</option>
        <option value="collapsed">折叠</option>
        <option value="minimized">最小化</option>
      </select>
    </div>

    <div class="actions">
      <button @click="saveSettings">保存</button>
      <button @click="$emit('close')">取消</button>
    </div>
  </div>
</template>
```

---

## 📦 四、集成方案

### 4.1 组件位置调整

```
ChatPanel.vue
├── MessageList.vue (保持不变)
│   ├── MessageItem.vue
│   │   ├── USRMessage.vue (用户消息)
│   │   ├── PMMessage.vue (PM 消息)
│   │   ├── SERichMessage.vue (SE 轻量级操作卡片) ⭐ 新设计
│   │   └── APMessage.vue (AP 审核报告)
│   └── ...
│
├── GlobalTaskBar.vue  ← 新增！放在输入框上方
│
└── InputBox.vue (输入框)
```

### **4.1.1 事件通信机制：ChatPanel ↔ 专业工具**

**核心架构图**:
```
┌─ ChatPanel (左侧) ─────────────────────────────┐
│                                                  │
│  SERichMessage.vue                              │
│    ├─ 点击文件名 → emit('open-file-in-editor')  │
│    ├─ 点击命令 → emit('run-in-terminal')        │
│    └─ 点击结果 → emit('view-output-in-terminal') │
│          ↓                                      │
│  ChatPanel.vue (事件中转)                        │
│    ├─ @open-file-in-editor="handleOpenFile"      │
│    └─ @run-in-terminal="handleRunCommand"        │
│          ↓                                      │
│  App.vue (中央调度)                              │
│    ├─ EventsEmit('editor:open-file', data)       │
│    └─ EventsEmit('terminal:execute', data)       │
│          ↓                                      │
├──────────────────────────────────────────────────┤
│  EditorWindow.vue (右侧)                         │
│    └─ EventsOn('editor:open-file', callback)     │
│        → 打开 Monaco 编辑器标签页                 │
│                                                  │
│  TerminalWindow.vue (右侧)                       │
│    └─ EventsOn('terminal:execute', callback)     │
│        → 在 xterm 终端执行命令                    │
└──────────────────────────────────────────────────┘
```

#### **完整数据流代码实现**

**Step 1: SERichMessage.vue - 发射事件**
```vue
<script setup lang="ts">
const emit = defineEmits([
  'open-file-in-editor',    // { path: string }
  'run-in-terminal',         // { command: string }
  'view-output-in-terminal',// { output: string, command: string }
])

function handleFileClick(path: string) {
  emit('open-file-in-editor', { path })
}

function handleCommandClick(command: string) {
  emit('run-in-terminal', { command })
}

function handleRunButtonClick(command: string) {
  emit('run-in-terminal', { command })
}
</script>
```

**Step 2: ChatPanel.vue - 中转事件**
```vue
<template>
  <div class="chat-panel">
    <MessageList 
      :messages="messages"
      @open-file-in-editor="(data) => $emit('open-file-in-editor', data)"
      @run-in-terminal="(data) => $emit('run-in-terminal', data)"
      @view-output-in-terminal="(data) => $emit('view-output-in-terminal', data)"
    />
    
    <GlobalTaskBar />
    <InputBox />
  </div>
</template>

<script setup lang="ts">
defineEmits([
  'open-file-in-editor',
  'run-in-terminal',
  'view-output-in-terminal',
])
</script>
```

**Step 3: App.vue - 中央调度**
```vue
<template>
  <div class="argus-app">
    <!-- 左侧：对话区 -->
    <ChatPanel
      :messages="messages"
      @open-file-in-editor="handleOpenFileInEditor"
      @run-in-terminal="handleRunInTerminal"
      class="chat-panel"
    />

    <!-- 右侧面板 -->
    <div class="right-panel">
      <EditorWindow 
        v-if="activeWindows.editor"
        :file="currentFile"
      />
      
      <TerminalWindow 
        v-if="activeWindows.terminal"
        :logs="terminalLogs"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'

onMounted(() => {
  // 初始化事件监听器（只初始化一次）
  initEventListeners()
})

async function handleOpenFileInEditor(data: { path: string }) {
  console.log('[App] 打开文件到编辑器:', data.path)
  
  // 1. 确保编辑器窗口打开
  activeWindows.editor = true
  
  // 2. 通过 Wails 读取文件内容
  const content = await ReadFile(data.path)
  
  // 3. 发送事件给 EditorWindow 组件
  EventsEmit('editor:open-file', {
    path: data.path,
    content: content,
    timestamp: new Date().toISOString()
  })
}

async function handleRunInTerminal(data: { command: string }) {
  console.log('[App] 在终端执行:', data.command)
  
  // 1. 确保终端窗口打开
  activeWindows.terminal = true
  
  // 2. 发送事件给 TerminalWindow 组件
  EventsEmit('terminal:execute', {
    command: data.command,
    timestamp: new Date().toISOString()
  })
}
</script>
```

**Step 4: EditorWindow.vue - 监听并响应**
```vue
<template>
  <div class="editor-window">
    <EditorArea 
      :tabs="tabs"
      :active-tab="activeTab"
      @tab-change="activeTab = $event"
    />
  </div>
</template>

<script setup lang="ts">
import { EventsOn } from '../../wailsjs/runtime/runtime'

interface Tab {
  id: string
  name: string
  content: string
  path?: string
  modified?: boolean
}

const tabs = ref<Tab[]>([])
const activeTab = ref<string>('')

onMounted(() => {
  // 监听来自 App.vue 的文件打开事件
  EventsOn('editor:open-file', (data: { path: string, content: string }) => {
    console.log('[Editor] 收到打开文件请求:', data.path)
    
    // 检查是否已打开该文件
    const existingTab = tabs.value.find(t => t.path === data.path)
    
    if (existingTab) {
      // 已存在则切换到该标签
      activeTab.value = existingTab.id
    } else {
      // 不存在则新建标签
      const newTab: Tab = {
        id: generateId(),
        name: getFileName(data.path),
        content: data.content,
        path: data.path,
        modified: false
      }
      
      tabs.value.push(newTab)
      activeTab.value = newTab.id
      
      // 自动滚动到编辑器区域
      nextTick(() => {
        focusEditor()
      })
    }
  })
})

function generateId(): string {
  return `tab-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

function getFileName(path: string): string {
  return path.split(/[/\\]/).pop() || 'untitled'
}
</script>
```

**Step 5: TerminalWindow.vue - 监听并执行**
```vue
<template>
  <div class="terminal-window">
    <XtermTerminal 
      ref="xtermRef"
      title="终端"
      :theme="'dark'"
      @command="handleUserCommand"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { ExecuteCommand } from '../../wailsjs/go/main/App'

const xtermRef = ref()

onMounted(() => {
  // 监听来自 App.vue 的执行命令事件
  EventsOn('terminal:execute', async (data: { command: string }) => {
    console.log('[Terminal] 收到执行命令请求:', data.command)
    
    await executeCommandInTerminal(data.command)
  })
})

async function executeCommandInTerminal(command: string) {
  // 1. 在 xterm 中显示命令（模拟用户输入）
  if (xtermRef.value?.writeToTerminal) {
    xtermRef.value.writeToTerminal(`\r\n$ ${command}\r\n`)
  }
  
  try {
    // 2. 调用后端执行命令
    const result = await ExecuteCommand(command)
    
    // 3. 流式输出结果到终端
    if (result.output && xtermRef.value?.writeToTerminal) {
      xtermRef.value.writeToTerminal(result.output)
    }
    
    // 4. 显示退出码
    if (xtermRef.value?.writeToTerminal) {
      const exitIcon = result.exitCode === 0 ? '✅' : '❌'
      xtermRef.value.writeToTerminal(
        `\r\n${exitIcon} Exit code: ${result.exitCode} (${result.duration || '0s'})\r\n\n$ `
      )
    }
    
    // 5. 记录日志
    terminalLogs.value.push({
      timestamp: new Date().toISOString(),
      command: command,
      exitCode: result.exitCode,
      duration: result.duration
    })
    
  } catch (error) {
    console.error('[Terminal] 执行命令失败:', error)
    
    if (xtermRef.value?.writeToTerminal) {
      xtermRef.value.writeToTerminal(
        `\r\n❌ Error: ${error.message}\r\n$ `
      )
    }
  }
}

// 处理用户在终端中的手动输入
function handleUserCommand(command: string) {
  executeCommandInTerminal(command)
}
</script>
```

#### **事件类型定义汇总**

```typescript
// types/events.ts

/** 文件操作事件 */
interface OpenFileInEditorEvent {
  type: 'open-file-in-editor'
  payload: {
    path: string           // 文件绝对路径
  }
}

/** 命令执行事件 */
interface RunInTerminalEvent {
  type: 'run-in-terminal'
  payload: {
    command: string         // 要执行的命令
  }
}

/** 输出查看事件 */
interface ViewOutputInTerminalEvent {
  type: 'view-output-in-terminal'
  payload: {
    command: string         // 原始命令
    output: string          // 完整输出
    exitCode: number        // 退出码
  }
}

/** 编辑器内部事件 */
interface EditorOpenFileEvent {
  type: 'editor:open-file'
  payload: {
    path: string
    content: string
    timestamp: string
  }
}

/** 终端内部事件 */
interface TerminalExecuteEvent {
  type: 'terminal:execute'
  payload: {
    command: string
    timestamp: string
  }
}

type AppEvent = 
  | OpenFileInEditorEvent
  | RunInTerminalEvent
  | ViewOutputInTerminalEvent
  | EditorOpenFileEvent
  | TerminalExecuteEvent
```

### 4.2 现有三层模型的处理

#### **方案 A：完全移除 TaskList 层（推荐）**

**修改前**:
```
RichMessage.vue
├── TaskListBlock.vue  ← 删除
├── ShellBlock.vue     ← 保留
└── ResultBlock.vue    ← 保留
```

**修改后**:
```
SimpleRichMessage.vue
├── ShellBlock.vue     ← 保留并增强
└── ResultBlock.vue    ← 保留
```

**理由**:
- TaskList 功能已迁移到全局任务栏
- 消息内只保留实时操作日志（Shell）和结果（Result）
- 减少信息冗余

#### **方案 B：保留但隐藏（备选）**

如果某些场景还需要消息内显示 TaskList：
- 默认不渲染 TaskList
- 通过配置项控制是否显示
- 数据仍然生成但不展示

### 4.3 后端修改点

#### **4.3.1 manager.go 修改**

```go
// 在 PM 分配任务时自动创建全局任务
func (m *Manager) handlePMReviewWithRich(content string, pmCtx *PMContext) error {
    // 解析任务描述
    taskDesc := extractTaskDescription(content)
    
    // 创建全局任务
    task, err := m.taskManager.CreateTask(taskDesc, "PM")
    if err != nil {
        log.Printf("[ERROR] Failed to create global task: %v", err)
    }
    
    // 更新任务状态为进行中
    m.taskManager.UpdateTaskStatus(task.ID, "doing")
    
    // ... 原有逻辑
    
    // SE 完成后标记任务完成
    defer func() {
        m.taskManager.UpdateTaskStatus(task.ID, "done", "已完成")
    }()
    
    return nil
}

// 在 SE 执行时更新进度
func (m *Manager) executeSEActions(actions []Action, seCtx *SEContext) error {
    for i, action := range actions {
        // 更新进度
        progress := fmt.Sprintf("%d/%d %s", i+1, len(actions), action.Type)
        m.taskManager.UpdateTaskStatus(currentTaskID, "doing", progress)
        
        // ... 执行逻辑
    }
    
    return nil
}
```

#### **4.3.2 app.go 修改**

```go
type App struct {
    // ... 现有字段
    
    taskManager *TaskManager  // 新增
}

func NewApp(config *config.Config) (*App, error) {
    app := &App{
        // ... 现有初始化
        
        taskManager: NewTaskManager(sseServer),  // 新增
    }
    
    // 注册任务相关路由
    app.setupTaskRoutes()
    
    return app, nil
}
```

---

## 🗓️ 五、实施计划

### Phase 1: 基础框架（预计 2 小时）

**目标**: 实现基本的全局任务栏 UI 和数据流

#### **任务清单**:

- [ ] **1.1 创建数据模型**
  - [ ] 定义 TypeScript 接口 (`types/task.ts`)
  - [ ] 定义 Go 结构体 (`internal/types/types.go`)
  - [ ] 编写单元测试

- [ ] **1.2 实现后端 TaskManager**
  - [ ] 创建 `internal/task/manager.go`
  - [ ] 实现 CRUD 操作
  - [ ] 实现并发安全（读写锁）
  - [ ] 编写测试用例

- [ ] **1.3 扩展 SSE 事件**
  - [ ] 定义新的 TaskEvent 类型
  - [ ] 实现推送函数
  - [ ] 测试 SSE 广播

- [ ] **1.4 创建前端 Pinia Store**
  - [ ] 创建 `stores/taskStore.ts`
  - [ ] 实现状态管理
  - [ ] 实现 SSE 监听
  - [ ] 编写测试

- [ ] **1.5 创建 GlobalTaskBar 组件**
  - [ ] 创建 `components/chat/GlobalTaskBar.vue`
  - [ ] 实现基础 UI（标题栏 + 任务列表）
  - [ ] 实现三种显示状态
  - [ ] 实现基础交互（折叠、最小化、关闭）

**验收标准**:
- ✅ 可以手动添加任务到任务栏
- ✅ 任务可以正确显示和更新
- ✅ 三种显示状态切换正常
- ✅ 无明显 bug

---

### Phase 2: 核心功能集成（预计 3 小时）

**目标**: 将任务栏与现有业务逻辑深度集成

#### **任务清单**:

- [ ] **2.1 PM 角色集成**
  - [ ] 修改 `manager.go` 的 PM 处理逻辑
  - [ ] PM 接收任务时自动创建全局任务
  - [ ] PM 分配给 SE 时更新任务状态
  - [ ] PM 审核完成后标记任务完成
  - [ ] 测试完整流程

- [ ] **2.2 SE 角色集成**
  - [ ] 修改 SE 执行逻辑
  - [ ] 每个 Action 开始时更新进度
  - [ ] Shell 输出关联到任务
  - [ ] 错误处理和失败标记
  - [ ] 测试完整流程

- [ ] **2.3 AP 角色集成**
  - [ ] AP 接收审核任务时创建任务
  - [ ] AP 审核完成后标记完成
  - [ ] 测试完整流程

- [ ] **2.4 简化现有三层模型**
  - [ ] 移除 RichMessage 的 TaskListBlock
  - [ ] 创建 SimpleRichMessage 组件
  - [ ] 只保留 Shell + Result 两层
  - [ ] 确保向后兼容

- [ ] **2.5 ChatPanel 集成**
  - [ ] 在 ChatPanel 中引入 GlobalTaskBar
  - [ ] 放置在正确位置（输入框上方）
  - [ ] 调整布局和间距
  - [ ] 响应式适配

**验收标准**:
- ✅ PM → SE → AP 流程中任务自动创建和更新
- ✅ 任务栏实时反映当前工作状态
- ✅ 简化后的消息展示正常
- ✅ 无性能问题

---

### Phase 3: 高级特性（预计 2 小时）

**目标**: 实现高级交互和优化体验

#### **任务清单**:

- [ ] **3.1 设置面板**
  - [ ] 实现完整的 SettingsPanel 组件
  - [ ] 角色过滤功能
  - [ ] 最大任务数限制
  - [ ] 自动清除已完成任务
  - [ ] 本地存储设置（localStorage）

- [ ] **3.2 任务详情跳转**
  - [ ] 点击任务滚动到对应消息
  - [ ] 高亮关联消息
  - [ ] 双击打开详情弹窗（可选）

- [ ] **3.3 动画和过渡效果**
  - [ ] 新任务滑入动画
  - [ ] 状态变更过渡
  - [ ] 脉冲效果优化
  - [ ] 性能优化（避免过多动画）

- [ ] **3.4 错误处理和边界情况**
  - [ ] SSE 断线重连机制
  - [ ] 任务数据持久化（可选）
  - [ ] 大量任务时的性能优化
  - [ ] 异常状态处理（网络错误等）

- [ ] **3.5 快捷键支持**
  - [ ] Ctrl/Cmd + T 切换任务栏显隐
  - [ ] Escape 关闭设置面板
  - [ ] 其他常用快捷键

**验收标准**:
- ✅ 设置面板功能完整可用
- ✅ 点击任务能正确跳转
- ✅ 动画流畅无卡顿
- ✅ 边界情况处理得当
- ✅ 快捷键响应灵敏

---

### Phase 4: 测试和优化（预计 1 小时）

**目标**: 全面测试和性能优化

#### **任务清单**:

- [ ] **4.1 功能测试**
  - [ ] 单元测试覆盖率 > 80%
  - [ ] 集成测试关键路径
  - [ ] E2E 测试主要场景
  - [ ] 回归测试现有功能

- [ ] **4.2 性能测试**
  - [ ] 大量任务（100+）时的性能
  - [ ] 频繁更新的性能
  - [ ] 内存泄漏检测
  - [ ] 渲染性能优化

- [ ] **4.3 兼容性测试**
  - [ ] 不同分辨率下的显示
  - [ ] 不同操作系统（Windows/Mac/Linux）
  - [ ] 浏览器兼容性（WebView2 版本）

- [ ] **4.4 用户验收测试**
  - [ ] 模拟真实使用场景
  - [ ] 收集反馈
  - [ ] 修复发现的问题

- [ ] **4.5 文档编写**
  - [ ] 更新 README（如需要）
  - [ ] 编写使用指南
  - [ ] 记录已知限制

**验收标准**:
- ✅ 所有测试通过
- ✅ 性能达到预期
- ✅ 无严重 bug
- ✅ 用户满意度高

---

## 🧪 六、测试策略

### 6.1 单元测试

#### **后端测试**

```go
// internal/task/manager_test.go

func TestCreateTask(t *testing.T) {
    tm := NewTaskManager(nil)
    
    task, err := tm.CreateTask("写入文件", "PM")
    assert.NoError(t, err)
    assert.Equal(t, "pending", task.Status)
    assert.Equal(t, "PM", task.Role)
}

func TestUpdateTaskStatus(t *testing.T) {
    tm := NewTaskManager(nil)
    task, _ := tm.CreateTask("运行程序", "SE")
    
    err := tm.UpdateTaskStatus(task.ID, "doing", "编译中...")
    assert.NoError(t, err)
    assert.Equal(t, "doing", task.Status)
    assert.Equal(t, "编译中...", task.Progress)
}

func TestConcurrentAccess(t *testing.T) {
    tm := NewTaskManager(nil)
    
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            tm.CreateTask(fmt.Sprintf("任务%d", i), "PM")
        }()
    }
    
    wg.Wait()
    assert.Equal(t, 100, len(tm.GetAllTasks()))
}
```

#### **前端测试**

```typescript
// __tests__/taskStore.test.ts

describe('TaskStore', () => {
  it('should add task correctly', () => {
    const store = useTaskStore()
    store.addTask({
      description: '测试任务',
      role: 'PM',
      status: 'pending',
    })
    
    expect(store.tasks).toHaveLength(1)
    expect(store.tasks[0].description).toBe('测试任务')
  })

  it('should update task status', () => {
    const store = useTaskStore()
    store.addTask({ description: '任务', role: 'SE', status: 'pending' })
    store.updateTask(store.tasks[0].id, { status: 'done' })
    
    expect(store.tasks[0].status).toBe('done')
  })

  it('should compute stats correctly', () => {
    const store = useTaskStore()
    store.addTask({ desc: 'T1', role: 'PM', status: 'doing' })
    store.addTask({ desc: 'T2', role: 'SE', status: 'done' })
    
    expect(store.taskStats.active).toBe(1)
    expect(store.taskStats.completed).toBe(1)
  })
})
```

### 6.2 集成测试场景

#### **场景 1: 完整工作流**

```
1. 用户发送: "hello pls write a hello world go program and run"

2. 预期行为:
   - PM 创建任务: "创建 Hello World 程序" [PM] ○ → ▶
   - PM 分配给 SE
   
3. SE 执行:
   - 任务更新: "写入 hello.go 文件" [SE] ▶ ... (write_file)
   - 任务更新: "运行程序验证" [SE] ▶ ... (exec go run)
   - 任务完成: "运行程序验证" [SE] ✓
   
4. PM 审核:
   - 任务更新: "最终质量审批" [AP] ○ → ▶
   
5. AP 审批:
   - 任务完成: "最终质量审批" [AP] ✓

6. 最终状态:
   - 所有任务显示为 ✓
   - 任务栏可以收起
```

#### **场景 2: 错误处理**

```
1. SE 执行失败（语法错误）

2. 预期行为:
   - 任务状态变为 failed [SE] ✗
   - 显示错误信息
   - 用户可以看到哪里出错
   - 可以重试或取消
```

#### **场景 3: 大量任务**

```
1. 连续发送多个复杂任务

2. 预期行为:
   - 任务栏正常显示（最多 N 个）
   - 已完成的自动归档
   - 滚动流畅无卡顿
   - 内存占用合理
```

### 6.3 E2E 测试（可选）

使用 Playwright 或 Cypress 进行端到端测试：

```typescript
// e2e/global-task-bar.spec.ts

test('should show task bar on message send', async ({ page }) => {
  await page.goto('/')
  
  await page.fill('[data-testid="input"]', 'create a file')
  await page.click('[data-testid="send"]')
  
  await expect(page.locator('[data-testid="task-bar"]')).toBeVisible()
  await expect(page.locator('.task-item')).toHaveCount(1)
})

test('should update task status in real-time', async ({ page }) => {
  // 发送任务
  // 等待状态变化
  // 验证 UI 更新
})
```

---

## 🚨 七、风险和缓解措施

### 7.1 技术风险

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|---------|
| SSE 连接不稳定 | 任务更新延迟 | 中 | 实现断线重连；本地缓存状态 |
| 大量任务导致性能问题 | UI 卡顿 | 低 | 虚拟滚动；限制最大数量；分页加载 |
| 并发竞争条件 | 数据不一致 | 低 | 使用读写锁；事务性更新 |
| 前后端数据不同步 | 显示错误 | 中 | 强制刷新机制；版本号校验 |

### 7.2 业务风险

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|---------|
| 用户不接受新布局 | 学习成本高 | 低 | 提供开关选项；渐进式推出 |
| 与现有功能冲突 | bug 增多 | 中 | 充分的回归测试；灰度发布 |
| AI 生成的任务质量差 | 任务无意义 | 中 | 优化 prompt；人工审核示例 |

### 7.3 缓解策略

1. **Feature Flag 控制**
   ```javascript
   const ENABLE_GLOBAL_TASK_BAR = true  // 可以快速关闭
   ```

2. **回滚方案**
   - 保留原有三层模型代码
   - 通过配置切换新旧版本
   - 数据库迁移脚本准备好

3. **监控和告警**
   - 监控 SSE 连接成功率
   - 监控任务创建/更新频率
   - 异常情况自动报警

---

## 📊 八、成功指标

### 8.1 技术指标

| 指标 | 目标值 | 测量方法 |
|------|--------|---------|
| 任务创建延迟 | < 100ms | 从 PM 接收到任务显示 |
| 状态更新延迟 | < 200ms | 从 SE 执行到 UI 更新 |
| 渲染性能 | < 16ms/frame | 60fps 流畅度 |
| 内存增长 | < 10MB/hour | 长时间运行稳定性 |
| 测试覆盖率 | > 80% | 单元测试 + 集成测试 |

### 8.2 业务指标

| 指标 | 目标值 | 测量方法 |
|------|--------|---------|
| 用户满意度 | > 4.0/5.0 | 用户反馈调查 |
| 任务可见性提升 | +50% | 用户调研（找到任务的时间） |
| 信息冗余减少 | -30% | 对比前后消息长度 |
| 用户采用率 | > 70% | 使用任务栏功能的用户比例 |

---

## 🔄 九、迭代计划

### Version 1.0 (MVP)
- ✅ 基础任务栏 UI
- ✅ PM/SE/AP 集成
- ✅ 实时更新
- ✅ 基本交互（折叠、关闭）

### Version 1.1
- ⚙️ 设置面板
- 🔍 任务搜索和过滤
- 📊 任务统计图表
- 💾 任务历史记录

### Version 1.2
- 🎯 子任务支持
- 📌 重要任务置顶
- 👥 多人协作（未来）
- 📱 移动端优化

### Version 2.0
- 🤖 AI 自动生成更智能的任务描述
- 📈 任务预估时间
- 🎨 自定义主题
- 🔌 插件系统（未来）

---

## 📝 十、附录

### 10.1 术语表

| 术语 | 定义 |
|------|------|
| **全局任务栏** | 固定在对话框底部的统一任务展示区域 |
| **SSE** | Server-Sent Events，服务器推送技术 |
| **Pinia** | Vue 3 的状态管理库 |
| **任务生命周期** | pending → doing → done/failed |
| **角色标记** | [PM]/[SE]/[AP]/[USR] 标识责任归属 |

### 10.2 参考资源

- [Trae IDE](https://trae.ai/) - 任务栏设计参考
- [Cursor](https://cursor.sh/) - AI 编辑器 UX 参考
- [Vue 3 文档](https://vuejs.org/) - 前端框架
- [Pinia 文档](https://pinia.vuejs.org/) - 状态管理
- [Go SSE 库](https://github.com/r3labs/sse) - 服务端推送

### 10.3 已知限制

1. **离线支持**: 当前版本需要网络连接才能实时更新
2. **历史限制**: 最多保留最近 100 个任务（可配置）
3. **浏览器兼容**: 基于 WebView2，需 Windows 10+
4. **并发上限**: 同时最多 50 个活跃任务

### 10.4 变更日志

| 日期 | 版本 | 作者 | 变更内容 |
|------|------|------|---------|
| 2026-05-21 | v1.0 | AI Assistant | 初稿完成：全局任务栏基础设计 |
| 2026-05-21 | v1.1 | AI Assistant | **重大更新**：新增轻量级操作卡片设计 + 与 Monaco 编辑器/xterm 终端深度集成方案 |

**v1.1 详细变更**:
- ✅ 新增 **第二章五节**: 对话活动区详细设计
  - 轻量级 SERichMessage 组件设计（替代消息内假终端）
  - 各角色消息详细设计（USR/PM/SE/AP）
  - 完整的 SCSS 样式规范
- ✅ 新增 **4.1.1 小节**: 事件通信机制
  - ChatPanel → Editor/Terminal 完整数据流
  - 5 步实现代码（SERichMessage → ChatPanel → App → EditorWindow → TerminalWindow）
  - TypeScript 事件类型定义
- ✅ 更新组件位置调整图（4.1节）
  - 新增 USRMessage、PMMessage、SERichMessage、APMessage 组件
  - 标注 ⭐ 新设计的 SERichMessage

---

## ✅ 十一、审批签字

| 角色 | 姓名 | 日期 | 签字 |
|------|------|------|------|
| 产品负责人 | | | |
| 技术负责人 | | | |
| 开发者 | | | |
| 测试者 | | | |

---

**文档结束**

> 💡 **提示**: 这是一个活的文档，随着项目进展会持续更新。
> 请将此文档保存在 `docs/GLOBAL-TASK-BAR-DESIGN.md` 以便团队查阅。
