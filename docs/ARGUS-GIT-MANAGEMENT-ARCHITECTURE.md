# Argus IDE Git管理架构与AutoSave机制 - 权威设计文档

> **文档版本**: v1.0  
> **创建日期**: 2026-05-28  
> **作者**: Argus开发团队  
> **状态**: 正式发布 ✅  
> **重要程度**: ⭐⭐⭐⭐⭐ (核心架构文档，所有开发者必读)

---

## 📖 目录

1. [背景与问题起源](#1-背景与问题起源)
2. [核心概念与架构](#2-核心概念与架构)
3. ["女婿上丈母娘的床"事件详析](#3-女婿上丈母娘的床事件详析)
4. [Git管理策略设计](#4-git管理策略设计)
5. [技术实现方案](#5-技术实现方案)
6. [安全机制](#6-安全机制)
7. [用户操作指南](#7-用户操作指南)
8. [故障排查手册](#8-故障排查手册)
9. [未来演进方向](#9-未来演进方向)
10. [附录](#10-附录)

---

## 1. 背景与问题起源

### 1.1 项目定位

**Argus是什么？**

```
┌─────────────────────────────────────────────────────┐
│  Argus = Vibe Coding IDE 编程平台                    │
│                                                     │
│  一个基于AI的集成开发环境，让用户通过自然语言描述需求 │
│  由系统自动拆解任务、分配角色、执行代码、完成项目     │
│                                                     │
│  开发平台: Trae IDE                                  │
│  目标用户: 非专业程序员 / 快速原型开发者             │
│  核心特色: PM→SE→AP 三角色协作流程                   │
└─────────────────────────────────────────────────────┘
```

### 1.2 三层架构模型

Argus作为一个IDE产品，天然存在**三层工作空间**：

#### 第1层：IDE开发层（开发者视角）

```bash
位置: E:\ArgusTek\Argus\
用途: 存放Argus IDE本身的源代码
Git: github.com/argustek/Argus.git
管理者: 开发团队（你）

文件结构:
├── app.go                 # 应用主入口
├── internal/              # 核心业务逻辑
│   ├── chat/              # 聊天管理器
│   ├── monitor/           # C监控模块
│   ├── ai/                # AI提示词
│   └── task/              # 任务管理
├── frontend/              # 前端界面
│   └── src/
│       ├── App.vue        # 主应用组件
│       └── components/    # UI组件
└── build/bin/             # 编译产物
    └── argus-desktop.exe  # 可执行文件
```

**关键点**: 这一层是**IDE本身**，就像VS Code的源代码一样。

---

#### 第2层：IDE运行时配置层（系统视角）

```bash
位置: E:\TempArgusTest\config\  (在用户工作目录内)
用途: Argus IDE的运行状态和配置
管理者: Argus IDE自动管理（用户一般不直接操作）

文件结构:
E:\TempArgusTest\
├── config/
│   └── config.json         # API配置、HTTP端口等
├── .argus/                  # IDE内部数据目录
│   ├── conversation.log     # 对话历史记录
│   ├── messages.json        # 消息存储
│   ├── state.json           # 当前状态快照
│   ├── task_memory.json     # 任务记忆
│   ├── route.log            # 路由日志
│   ├── env_memory.json      # 环境记忆
│   ├── resets/              # 重置记录归档
│   │   └── reset_20260528_181302.json
│   ├── permission_config.json
│   └── decision_config.json
└── argus.log                # 运行日志
```

**关键点**: 这些是**IDE的私房钱**，包含敏感信息（API Key等），绝不能泄露到远程仓库！

---

#### 第3层：用户工作目录（用户视角） ⭐⭐⭐

```bash
位置: E:\TempArgusTest\ (用户选择的工作目录)
用途: 用户用Argus IDE编写代码的地方
Git: 用户自己的仓库（或无Git）
管理者: 用户自己 + Argus辅助管理

文件结构:
E:\TempArgusTest\
├── hello.go                 # 用户写的代码 ✅
├── main.go                  # 用户的项目文件 ✅
├── README.md                # 用户的项目文档 ✅
├── .git/                     # 用户自己的版本控制（可选）
│   └── config
│       └── ...              # remote指向用户的仓库
├── config/                   # IDE配置（第2层）
│   └── config.json
└── .argus/                   # IDE内部数据（第2层）
    └── ...
```

**关键点**: 这是**用户的地盘**，Argus只是工具，不能喧宾夺主！

---

### 1.3 核心矛盾

**问题的本质：三层空间如何和平共处？**

```
冲突场景：
┌─────────────────────────────────────────┐
│ 第1层 (IDE代码)                          │
│ Git: argustek/Argus.git                 │
│                                         │
│          ↓ 开发者修改代码               │
│                                         │
│ 第2层 (IDE配置)                          │
│ 位置: 用户工作目录内                      │
│ 包含: API Key, 运行状态                  │
│                                         │
│          ↓ C监控需要auto-save            │
│                                         │
│ 第3层 (用户代码)                         │
│ Git: ???                                │
│ 问题: auto-save应该commit到哪里？         │
└─────────────────────────────────────────┘
```

**传统IDE的解法：**
- VS Code: 不管理用户Git，只提供编辑功能
- JetBrains: 在项目目录初始化Git，但明确区分
- **Argus的特殊性**: 有C监控模块会自动保存状态！

---

## 2. 核心概念与架构

### 2.1 角色定义

| 角色 | 身份 | 工作范围 | Git权限 |
|------|------|----------|---------|
| **开发者(You)** | 丈母娘 | 第1层: E:\ArgusTek\Argus\ | 完全控制 |
| **Argus IDE** | 媳妇 | 第2+3层: E:\TempArgusTest\ | 辅助管理 |
| **User(USR)** | 女婿 | 第3层: 用户代码 | 主导权 |

### 2.2 Git职责划分

```
┌─────────────────────────────────────────────────────┐
│  Git仓库类型                                        │
├─────────────────────────────────────────────────────┤
│                                                     │
│  🏠 IDE主仓库                                       │
│  ├─ 路径: E:\ArgusTek\Argus\.git\                  │
│  ├─ Remote: github.com/argustek/Argus.git          │
│  ├─ 用途: 管理IDE源代码                             │
│  └─ 操作者: 开发者                                  │
│                                                     │
│  👤 用户项目仓库                                     │
│  ├─ 路径: E:\TempArgusTest\.git\ (如果存在)        │
│  ├─ Remote: 用户自己决定                            │
│  ├─ 用途: 管理用户编写的代码                        │
│  └─ 操作者: 用户主导 + Argus辅助                    │
│                                                     │
│  🔒 Argus备份仓库 (NEW!)                            │
│  ├─ 路径: E:\TempArgusTest\.argus\backup-git\.git\  │
│  ├─ Remote: 无 (纯本地)                             │
│  ├─ 用途: AutoSave备份                              │
│  └─ 操作者: Argus IDE完全控制                       │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### 2.3 AutoSave的定义与价值

**什么是AutoSave？**

```
传统AutoSave:
  → VS Code: 自动保存文件到磁盘 (Ctrl+S的自动化)

Argus的AutoSave (增强版):
  → 不仅保存文件，还保存完整的版本历史
  → 每5分钟检测变化 → 备份到独立仓库
  → 可以回滚到任意时间点
  → 类似 "时光机" 功能
```

**为什么需要AutoSave？**

| 场景 | 没有AutoSave | 有AutoSave |
|------|-------------|-----------|
| 用户误删文件 | ❌ 无法恢复 | ✅ 从备份恢复 |
| 代码改坏了 | ❌ 只能重写 | ✅ 回滚到之前版本 |
| SE执行出错 | ❌ 结果丢失 | ✅ 可以查看执行前的状态 |
| 长时间工作断电 | ❌ 丢失所有未保存内容 | ✅ 5分钟前已自动保存 |
| 需要对比修改 | ❌ 手动diff | ✅ git log查看完整历史 |

---

## 3. "女婿上丈母娘的床"事件详析

### 3.1 事件概述

**发生时间**: 2026年5月28日前后  
**严重等级**: 🔴 高危（可能导致API Key泄露）  
**影响范围**: 工作目录E:\TempArgusTest  
**发现方式**: 用户反馈 + Git状态检查  

**一句话总结**: 
> Argus IDE的C监控在工作目录中错误地创建了一个指向IDE主仓库的Git，导致627个auto-save commit差点被push到远程。

---

### 3.2 事件时间线还原

```
T-??:?? (未知时刻) 
  ↓
某个操作导致 E:\TempArgusTest\.git 被创建
可能原因：
  A. 用户在Git窗口中Clone了IDE仓库到工作目录
  B. 程序bug意外调用了GitInit
  C. 其他未知原因
  
T-??:??
  ↓
Trae IDE启动，扫描工作区
弹出提示："发现Git仓库 (remote: argustek/Argus)，是否加载？"
用户点击了 [加载] 或 [确定]

T-05-09 ~ T-05-28 (持续19天)
  ↓
C监控每5分钟执行autoCommit()
  → git add .
  → git commit -m "auto save by Argus-C (时间)"
  → 全部commit到了错误的仓库！
  
累积结果:
  • 627个auto-save commit
  • 100+个reset记录
  • 包含: hello.go, config/config.json (含API Key!), .argus/* 等

T-05-28 18:30
  ↓
用户发现问题，开始排查

T-05-28 19:00
  ↓
确认问题性质:
  ✅ 本地有627个commit (ahead 627)
  ❌ 但未push到远程 (万幸!)
  ⚠️ 包含敏感信息 (加密的API Key)
```

---

### 3.3 证据链

#### 证据1: 错误的Remote配置

```ini
# E:\TempArgusTest\.git\config
[core]
    repositoryformatversion = 0
    filemode = false
    bare = false

[branch "master"]
    remote = origin
    merge = refs/heads/master

[remote "origin"]
    url = https://github.com/argustek/Argus.git  ← ❌❌❌ 这里！
    fetch = +refs/heads/*:refs/remotes/origin/*
```

**问题**: 工作目录的Git指向了IDE主仓库！

---

#### 证据2: 627个Commit的内容分析

```bash
$ git log --all --name-only --pretty=format:"" | 统计结果:

✅ 安全文件 (90%):
  .argus/conversation.log      (1820次)  ← 对话日志
  .argus/messages.json                    ← 消息记录
  .argus/task_memory.json                 ← 任务记忆
  .argus/state.json                       ← 状态快照
  .argus/route.log                        ← 路由日志
  .argus/resets/reset_*.json             (100+) ← 重置记录

✅ 用户测试代码 (5%):
  hello.go, main.go, basic_demo.go 等

⚠️ 敏感文件 (5%):
  config/config.json                     ← 含加密API Key!
  argus.exe                              ← 编译产物

🔴 未发现:
  app.go, manager.go, monitor.go         ← 幸好没有IDE源码!
  internal/, frontend/                   ← 幸好没有IDE源码!
```

**结论**: 
- ✅ 只是上了床 (本地commit)
- ❌ 还没睡进去 (未push)
- ⚠️ 扒了裤子 (包含API Key)

---

#### 证据3: 性质判定

| 行为 | 判定 | 严重度 |
|------|------|--------|
| Clone/Init到工作目录 | ✅ 已确认 | 中高 |
| C监控autoCommit | ✅ 正常行为 | 无责 |
| Commit到错误的仓库 | ✅ 已确认 | 高危 |
| Push到远程 | ❌ **未发生** | 万幸! |

**最终定性**: 
> 🔴 **高危事件但未造成实际损失**
> 类比: 已经脱裤子准备上床，但在最后关头被制止

---

### 3.4 根因分析 (5 Whys)

```
Why 1: 为什么会有627个commit在错误的仓库？
  → 因为C监控的autoCommit()函数在工作目录执行了git commit

Why 2: 为什么会在那个工作目录执行？
  → 因为该目录存在.git，且指向了IDE主仓库

Why 3: 为什么那个.git会存在且指向错误？
  → 可能是用户在Git窗口中clone了IDE仓库到工作目录
     或程序某处调用了GitInit/GitClone

Why 4: 为什么没有安全检查？
  → GitClone()函数没有验证目标目录是否为workDir
  → autoCommit()没有检查remote的安全性

Why 5: 为什么设计时没考虑到这个问题？
  → 架构设计时未明确定义三层空间的Git边界
  → 缺少"IDE vs 用户"的权限隔离意识
```

**根本原因**: **架构设计缺陷 - 未定义清晰的Git管理边界**

---

## 4. Git管理策略设计

### 4.1 设计原则

基于上述事件教训，制定以下**铁律**：

#### 原则1: 三权分立

```
IDE主仓库 (R/W by 开发者)
  ↕ 完全隔离
用户项目仓库 (R/W by 用户, 辅助by IDE)
  ↕ 完全隔离
AutoSave备份仓库 (R/W by IDE only)
```

**任何情况下不得混淆这三个仓库！**

---

#### 原则2: 最小权限原则

```
Argus IDE对用户Git的操作权限:

✅ 允许:
  → 读取git status (显示给用户)
  → 读取git log (显示历史)
  → 执行用户明确请求的git命令 (commit/push/pull)

❌ 禁止:
  → 在用户仓库中自动执行commit (除非用户授权)
  → 修改用户的remote配置
  → push到任何远程仓库 (除非用户明确请求)

🔒 特殊:
  → AutoSave只在自己的备份仓库中操作
```

---

#### 原则3: 数据隔离

```
敏感数据分类:

🔴 绝不能进入Git (即使是本地):
  → API Keys (即使加密)
  → 用户隐私数据
  → 临时文件 (*.tmp, *.swp)

🟡 可以进入AutoSave备份仓库:
  → 用户代码 (.go, .py, .js等)
  → 配置文件 (不含密钥)
  → 文档文件 (README.md等)

🟢 应该由用户自己管理的:
  → 所有用户项目的正式提交
  → 远程仓库的同步
```

---

### 4.2 最终方案: 独立备份仓库模式 (方案C)

#### 4.2.1 架构图

```
E:\TempArgusTest\ (用户工作目录)
│
├── .git/                          👤 用户项目仓库 (可选)
│   ├── HEAD
│   ├── config/
│   │   └── remote = 用户自己的仓库
│   └── objects/
│
├── .argus/                        🔒 IDE私有目录
│   ├── backup-git/                🆕 AutoSave备份仓库 (NEW!)
│   │   ├── .git/                  ← 独立的Git仓库!
│   │   │   ├── HEAD
│   │   │   ├── config/            ← 无remote配置!
│   │   │   └── objects/
│   │   │
│   │   ├── hello.go               ← 复制的用户文件
│   │   ├── main.go
│   │   └── README.md
│   │
│   ├── conversation.log
│   ├── messages.json
│   ├── state.json
│   └── ...
│
├── hello.go                       👤 用户原始文件
├── main.go
└── README.md
```

**关键特点**:
1. `.argus/backup-git/` 是一个**完全独立的Git仓库**
2. 它**没有任何remote** (纯本地)
3. 它**永远不会被push** 到任何地方
4. 它**完全由Argus IDE管理**，用户无需关心

---

#### 4.2.2 工作流程

```
正常工作流程:

1️⃣ 用户打开Argus IDE
   ↓
2️⃣ 选择或创建工作目录 (如 E:\TempArgusTest)
   ↓
3️⃣ 用户编写代码 (hello.go)
   ↓
4️⃣ C监控检测到文件变化 (每5分钟检查一次)
   ↓
5️⃣ AutoSave流程启动:
   │
   ├─ Step 1: 扫描变化的文件列表
   │   changedFiles = ["hello.go", "main.go"]
   │
   ├─ Step 2: 确保 .argus/backup-git/ 仓库存在
   │   if not exist → git init (纯本地)
   │
   ├─ Step 3: 复制变化的文件到备份目录
   │   cp hello.go → .argus/backup-git/hello.go
   │   cp main.go → .argus/backup-git/main.go
   │
   ├─ Step 4: 在备份仓库中执行Git操作
   │   cd .argus/backup-git/
   │   git add .
   │   git commit -m "backup 2026-05-28 14:05:00"
   │
   └─ Step 5: 完成！
       ✅ 用户文件不受影响
       ✅ 备份已完成
       ✅ 用户的.git (如果有) 完全干净
   
6️⃣ 用户继续工作...
   ↓ (5分钟后重复Step 4-5)

7️⃣ 用户想手动提交代码:
   │
   ├─ 方式A: 使用IDE的Git窗口
   │   git add hello.go
   │   git commit -m "feat: 添加hello world"
   │   git push origin main  → 推送到用户自己的仓库 ✅
   │
   └─ 方式B: 自己在终端操作
       (完全自主，IDE不干预)
```

---

#### 4.2.3 数据流图

```
┌──────────────┐     写入      ┌──────────────────┐
│   用户编辑    │ ──────────→ │  E:\TempArgusTest\ │
│  (IDE编辑器)  │             │   hello.go (原始)  │
└──────────────┘             └────────┬─────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    │                 │                 │
                    ▼                 ▼                 ▼
           ┌────────────┐    ┌────────────┐    ┌────────────┐
           │ 用户查看   │    │ C监控检测  │    │ 用户手动   │
           │ (git status)│    │ (变化扫描) │    │ (git commit)│
           └────────────┘    └─────┬──────┘    └─────┬──────┘
                                     │                 │
                                     ▼                 ▼
                           ┌─────────────────┐  ┌─────────────────┐
                           │ .argus/backup-  │  │ 用户项目.git    │
                           │ git/ (AutoSave) │  │ (用户自己的仓库) │
                           │                 │  │                 │
                           │ git add .       │  │ git add .       │
                           │ git commit      │  │ git commit      │
                           │ (仅本地)        │  │ (可push)        │
                           └────────┬────────┘  └────────┬────────┘
                                    │                     │
                                    ▼                     ▼
                           ┌─────────────────┐  ┌─────────────────┐
                           │ 🔄 永远不push!  │  │ ✅ 可push到     │
                           │ (纯本地备份)    │  │ 用户自己的远程   │
                           └─────────────────┘  └─────────────────┘
```

---

## 5. 技术实现方案

### 5.1 核心代码结构

#### 5.1.1 修改后的autoCommit函数

**文件**: `internal/monitor/c_monitor.go`  
**函数**: `autoCommit()` 和新增的 `ensureBackupRepo()` 

```go
// autoCommit 自动备份工作目录变化到独立仓库
// [FIX-20260528-GIT] 采用方案C: 独立备份仓库，完全不污染用户Git
func (c *CMonitor) autoCommit(now int64) {
    // 1. 时间间隔检查 (每5分钟)
    if now-c.lastAutoCommit < int64(c.commitInterval.Seconds()) {
        return
    }
    
    // 2. 检测工作目录变化
    changedFiles := c.getChangedFiles()
    if len(changedFiles) == 0 {
        return
    }
    
    fmt.Printf("[C] AutoSave: 检测到 %d 个文件变化\n", len(changedFiles))
    
    // 3. 准备备份目录
    backupDir := filepath.Join(c.workDir, ".argus", "backup-git")
    
    // 4. 确保备份仓库存在且健康
    if !c.ensureBackupRepo(backupDir) {
        fmt.Printf("[C] ⚠️ 备份仓库初始化失败，跳过本次AutoSave\n")
        return
    }
    
    // 5. 复制变化的文件到备份目录
    copiedCount := c.copyChangedFiles(c.workDir, backupDir, changedFiles)
    if copiedCount == 0 {
        return
    }
    
    // 6. 在备份仓库中执行Git操作
    if !c.commitToBackupRepo(backupDir) {
        fmt.Printf("[C] ⚠️ 备份commit失败\n")
        return
    }
    
    // 7. 更新时间戳
    c.lastAutoCommit = now
    fmt.Printf("[C] ✅ AutoSave成功: 备份了 %d 个文件\n", copiedCount)
}

// ensureBackupRepo 确保备份仓库存在且配置正确
func (c *CMonitor) ensureBackupRepo(backupDir string) bool {
    gitDir := filepath.Join(backupDir, ".git")
    
    // 情况1: 备份仓库不存在 → 创建
    if _, err := os.Stat(gitDir); os.IsNotExist(err) {
        fmt.Printf("[C] 📁 创建AutoSave备份仓库: %s\n", backupDir)
        
        // 创建目录
        if err := os.MkdirAll(backupDir, 0755); err != nil {
            fmt.Printf("[C] ❌ 创建备份目录失败: %v\n", err)
            return false
        }
        
        // 初始化Git仓库
        initCmd := exec.Command("git", "init")
        initCmd.Dir = backupDir
        initCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
        if out, err := initCmd.CombinedOutput(); err != nil {
            fmt.Printf("[C] ❌ git init失败: %v\n%s\n", err, string(out))
            return false
        }
        
        // 配置: 设置用户信息 (用于commit)
        c.configureBackupRepo(backupDir)
        
        // 创建初始commit
        initialCmd := exec.Command("git", "add", ".", "-f")  // -f 强制添加所有文件
        initialCmd.Dir = backupDir
        initialCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
        initialCmd.CombinedOutput()
        
        commitMsg := fmt.Sprintf("Initial backup - %s", time.Now().Format("2006-01-02 15:04:05"))
        commitCmd := exec.Command("git", "commit", "-m", commitMsg, "--allow-empty")
        commitCmd.Dir = backupDir
        commitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
        commitCmd.CombinedOutput()
        
        fmt.Printf("[C] ✅ 备份仓库初始化完成 (纯本地，无remote)\n")
        return true
    }
    
    // 情况2: 备份仓库已存在 → 验证安全性
    return c.verifyBackupRepoSafety(backupDir)
}
```

---

#### 5.1.2 安全验证函数

```go
// verifyBackupRepoSafety 验证备份仓库的安全性
func (c *CMonitor) verifyBackupRepoSafety(backupDir string) bool {
    configPath := filepath.Join(backupDir, ".git", "config")
    
    data, err := os.ReadFile(configPath)
    if err != nil {
        fmt.Printf("[C] ⚠️ 无法读取备份仓库config: %v\n", err)
        return false  // 保守处理: 不允许使用可疑仓库
    }
    
    configContent := string(data)
    
    // 危险检查1: 不能有任何remote
    if strings.Contains(configContent, "[remote ") || strings.Contains(configContent, "[remote\"") {
        fmt.Printf("[C] 🚨 危险！备份仓库不应有remote配置！\n")
        fmt.Printf("[C] 正在移除remote...\n")
        
        // 移除所有remote
        removeCmd := exec.Command("git", "remote", "remove", "origin")
        removeCmd.Dir = backupDir
        removeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
        removeCmd.CombinedOutput()
    }
    
    // 危险检查2: 确认不是符号链接或其他 trick
    realPath, _ := filepath.EvalSymlinks(backupDir)
    if !strings.HasPrefix(realPath, filepath.Clean(c.workDir)) {
        fmt.Printf("[C] 🚨 危险！备份目录可能是符号链接攻击！\n")
        return false
    }
    
    fmt.Printf("[C] ✅ 备份仓库安全验证通过\n")
    return true
}
```

---

#### 5.1.3 文件复制与过滤

```go
// copyChangedFiles 复制变化的文件到备份目录
// 过滤规则: 不复制敏感文件和临时文件
func (c *CMonitor) copyChangedFiles(srcDir, dstDir string, files []string) int {
    // 需要排除的文件模式
    excludePatterns := []string{
        ".git/",              // Git目录
        "node_modules/",      // Node依赖
        "__pycache__/",       // Python缓存
        "*.exe",              // 可执行文件
        "*.dll",              // 动态库
        "*.tmp",              // 临时文件
        "*.swp",              # Vim交换文件
        "~$",                 # Office临时文件
        ".DS_Store",          # macOS垃圾
        "Thumbs.db",          # Windows缩略图
    }
    
    // 需要排除的敏感文件 (即使在include列表中也跳过)
    sensitivePatterns := []string{
        "config.json",        # 可能包含API Key
        "*.key",
        "*.pem",
        "*.p12",
        "id_rsa*",
        "id_dsa*",
        ".env",
        ".env.local",
    }
    
    copiedCount := 0
    
    for _, file := range files {
        // 检查是否应排除
        shouldExclude := false
        
        for _, pattern := range excludePatterns {
            matched, _ := filepath.Match(pattern, file)
            if matched {
                shouldExclude = true
                break
            }
        }
        
        for _, pattern := range sensitivePatterns {
            matched, _ := filepath.Match(pattern, filepath.Base(file))
            if matched {
                shouldExclude = true
                fmt.Printf("[C] 🔒 跳过敏感文件: %s\n", file)
                break
            }
        }
        
        if shouldExclude {
            continue
        }
        
        // 执行复制
        srcPath := filepath.Join(srcDir, file)
        dstPath := filepath.Join(dstDir, file)
        
        // 确保目标目录存在
        dstDirPath := filepath.Dir(dstPath)
        os.MkdirAll(dstDirPath, 0755)
        
        // 复制文件
        if err := copyFile(srcPath, dstPath); err != nil {
            fmt.Printf("[C] ⚠️ 复制文件失败 %s: %v\n", file, err)
            continue
        }
        
        copiedCount++
    }
    
    return copiedCount
}

// copyFile 复制单个文件
func copyFile(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    return os.WriteFile(dst, data, 0644)
}
```

---

### 5.2 GitClone安全加固

**文件**: `app.go`  
**函数**: `GitClone()` 

```go
func (a *App) GitClone(url, dir, branch string) map[string]interface{} {
    // [安全检查] 防止Clone到工作目录
    absDir, _ := filepath.Abs(dir)
    absWorkDir, _ := filepath.Abs(a.config.WorkDir)
    
    fmt.Printf("[GitClone] 请求: url=%s, dir=%s\n", url, absDir)
    
    // 检查1: 是否是工作目录
    if strings.EqualFold(absDir, absWorkDir) || 
       strings.HasPrefix(strings.ToLower(absDir), strings.ToLower(absWorkDir)+string(filepath.Separator)) {
        
        errMsg := fmt.Sprintf(
            "⚠️ 不能Clone到工作目录(%s)！\n"+
            "这会导致AutoSave污染您的项目。\n"+
            "请选择其他目录。",
            absWorkDir,
        )
        
        fmt.Printf("[GitClone] ❌ %s\n", errMsg)
        return map[string]interface{}{
            "success": false,
            "error":   errMsg,
        }
    }
    
    // 检查2: 目标目录是否有重要文件
    if files, _ := os.ReadDir(absDir); len(files) > 0 {
        if _, err := os.Stat(filepath.Join(absDir, ".git")); err == nil {
            errMsg := fmt.Sprintf("⚠️ %s已是Git仓库！", absDir)
            return map[string]interface{}{
                "success": false,
                "error":   errMsg,
            }
        }
    }
    
    // 执行Clone
    r := git.Clone(url, dir, branch)
    return map[string]interface{}{
        "success": r.Success,
        "output":  r.Output,
        "error":   r.Error,
    }
}
```

---

### 5.3 配置项设计

在 `config.json` 中新增AutoSave相关配置:

```json
{
  "autoSave": {
    "enabled": true,
    "intervalMinutes": 5,
    "maxBackups": 100,           // 最多保留多少个备份点
    "excludePatterns": [         // 用户自定义排除规则
      "*.log",
      "temp/",
      "build/"
    ],
    "notifyOnBackup": true       // 是否通知用户
  },
  "git": {
    "manageUserGit": false,      // 是否帮助用户管理Git (默认false)
    "allowAutoCommitToUserRepo": false  // 是否允许auto-commit到用户仓库
  }
}
```

---

## 6. 安全机制

### 6.1 多层防护体系

```
Layer 1: 预防 (Prevention)
├── GitClone() 检查目标目录
├── GitInit() 检查是否为workDir
└── 前端UI警告提示

Layer 2: 检测 (Detection)  
├── autoCommit() 检查remote安全性
├── 定期扫描 .git/config 内容
└── 日志审计 (记录所有Git操作)

Layer 3: 隔离 (Isolation)
├── AutoSave使用独立仓库 (.argus/backup-git/)
├── 不设置任何remote
├── 敏感文件过滤
└── 符号链接检查

Layer 4: 恢复 (Recovery)
├── 一键清理脚本
├── 备份数据完整性校验
└── 用户可手动删除 .argus/backup-git/
```

---

### 6.2 危险场景应对

| 场景 | 检测方法 | 应对措施 |
|------|----------|----------|
| 用户Clone到workDir | 路径比较 | 阻止并提示 |
| 用户手动创建危险.git | autoCommit检查 | 阻止并警告 |
| 恶意符号链接 | EvalSymlinks | 拒绝操作 |
| 备份仓库被篡改 | config校验 | 重建仓库 |
| 磁盘空间不足 | 写入前检查 | 清理旧备份 |

---

### 6.3 敏感数据处理

```go
// 永远不应该被备份的文件类型
var neverBackupPatterns = []*regexp.Regexp{
    regexp.MustCompile(`^.*\.key$`),
    regexp.MustCompile(`^.*\.pem$`),
    regexp.MustCompile(`^.*\.p12$`),
    regexp.MustCompile(`^id_rsa.*$`),
    regexp.MustCompile(`^id_dsa.*$`),
    regexp.MustCompile(`^\.env$`),
    regexp.MustCompile(`^\.env\..*$`),
    regexp.MustCompile(`^credentials.*$`),
    regexp.MustCompile(`^secrets?.*$`),
}

// 即使匹配也要检查内容的文件
var contentCheckFiles = []string{
    "config.json",
    "settings.json",
    ".argus/config.json",
}

func isSensitiveFile(filename string, content []byte) bool {
    // 1. 文件名匹配
    for _, pattern := range neverBackupPatterns {
        if pattern.MatchString(filename) {
            return true
        }
    }
    
    // 2. 内容检查 (针对配置文件)
    for _, checkFile := range contentCheckFiles {
        if filepath.Base(filename) == checkFile {
            if containsSensitiveData(content) {
                return true
            }
        }
    }
    
    return false
}

func containsSensitiveData(data []byte) bool {
    sensitiveKeywords := []string{
        "api_key", "apikey", "api-key",
        "password", "passwd",
        "secret", "token",
        "private_key", "privatekey",
    }
    
    content := strings.ToLower(string(data))
    for _, keyword := range sensitiveKeywords {
        if strings.Contains(content, keyword) {
            return true
        }
    }
    
    return false
}
```

---

## 7. 用户操作指南

### 7.1 正常使用流程

#### 场景1: 新用户首次使用

```
1. 启动 Argus IDE
2. 选择工作目录: E:\MyProjects\NewProject
3. 开始编程...
   
   (后台自动进行:)
   ✓ 检测到工作目录无 .git/backup-git
   ✓ 自动创建 .argus/backup-git/ 仓库
   ✓ 每5分钟自动备份你的代码
   
4. 你完全不需要关心Git的事情！
```

---

#### 场景2: 有经验的用户 (已有Git仓库)

```
1. 你已经有了一个Git项目: E:\MyProjects\ExistingProject
   该项目已有 .git/ 并指向你的GitHub仓库
   
2. 用Argus IDE打开这个目录
   
3. 开始编程...

   (后台自动进行:)
   ✓ 检测到工作目录已有 .git/ (用户仓库)
   ✓ 创建独立的 .argus/backup-git/ (不影响你的.git)
   ✓ AutoSave只备份到 .argus/backup-git/
   
4. 你可以正常使用Git:
   $ git add .
   $ git commit -m "my changes"
   $ git push origin main
   → 只推送你的commit，不包含AutoSave ✅
```

---

#### 场景3: 回滚到之前的状态

```
你不小心改坏了代码，想回到30分钟前的版本:

方法A: 使用Argus IDE的回滚功能 (TODO: 前端实现)
   → 打开 "本地历史" 面板
   → 选择30分钟前的备份点
   → 点击 "恢复此版本"

方法B: 手动从备份恢复
   cd E:\YourProject\.argus\backup-git
   git log --oneline   # 查看备份历史
   git show <commit-hash>:hello.go > ../hello.go  # 恢复单个文件
   # 或
   git checkout <commit-hash> -- .  # 恢复所有文件到该版本
```

---

### 7.2 设置与自定义

在Argus IDE的设置页面中:

```
[Version Control]
├─ ☑ 启用AutoSave (默认开启)
│  └─ 间隔: [5] 分钟
│
├─ ☐ 通知我每次备份 (默认关闭)
│
├─ 排除规则:
│  [*.log ] [temp/] [build/] [+ 添加]
│
└─ 高级选项:
   ├─ 最大备份数量: [100]
   ├─ 单个备份最大大小: [50] MB
   └─ [清理所有备份] 按钮 (危险!)
```

---

### 7.3 故障自查清单

**问题: AutoSave不工作?**

```
□ 检查1: AutoSave是否启用?
  → 设置 → Version Control → 启用AutoSave

□ 检查2: 工作目录是否存在?
  → 状态栏显示当前工作目录路径

□ 检查3: 磁盘空间是否充足?
  → .argus/backup-git/ 会占用空间

□ 查看日志:
  → 打开 argus.log
  → 搜索 "[C]" 相关条目
  → 查看是否有错误信息
```

**问题: 看到Git相关的警告?**

```
警告: "检测到危险的Git配置"
  → 原因: 工作目录的.git指向了argustek/Argus
  → 解决: 删除工作目录的.git (保留文件)
  → 命令: Remove-Item -Recurse -Force .git

警告: "AutoSave备份失败"
  → 原因: 权限不足或磁盘满
  → 解决: 检查 .argus/backup-git/.git/ 的权限
```

---

## 8. 故障排查手册

### 8.1 常见问题诊断

#### 问题1: "女婿上床"重现 (最严重!)

**症状**:
```
工作目录的 .git 指向了 argustek/Argus.git
C监控不断commit到错误的仓库
```

**诊断步骤**:
```powershell
# 1. 检查remote
cd E:\YourWorkDir
git remote -v
# 如果看到: origin https://github.com/argustek/Argus.git
# → 就是这个问题！

# 2. 检查commit历史
git log --oneline -5
# 如果看到: auto save by Argus-C (...)
# → 确认是C监控干的

# 3. 检查是否已push
git status
# 如果看到: ahead N (表示未push)
# → 还来得及补救！
```

**修复方案**:
```powershell
# 步骤1: 备份重要配置 (如果有)
Copy-Item config\config.json config\config.json.backup

# 步骤2: 删除错误的.git
Remove-Item -Recurse -Force .git

# 步骤3: 验证
Test-Path .git  # 应返回 False

# 步骤4: 重启Argus IDE
# → 会自动创建正确的 .argus/backup-git/
```

**预防措施** (已实装):
- ✅ GitClone() 安全检查
- ✅ autoCommit() remote验证
- ✅ 独立备份仓库模式

---

#### 问题2: AutoSave占用太多磁盘空间

**症状**:
```
.argus/backup-git/ 目录很大 (>500MB)
```

**诊断**:
```powershell
# 查看备份仓库大小
cd .argus\backup-git
du -sh .

# 查看commit数量
git rev-list --count HEAD

# 查看大文件
git rev-list --objects --all | \
  git cat-file --batch-check='%(objecttype) %(objectname) %(objectsize) %(rest)' | \
  awk '/blob/ {print $3, $4}' | sort -rn | head -20
```

**解决方案**:
```powershell
# 方法1: 清理旧备份 (保留最近N个)
cd .argus\backup-git
# 保留最近50个commit，删除更早的
git checkout --orphan temp
git commit -m "cleanup"
git rebase --onto temp $(git rev-list HEAD | tail -n 51 | head -1)
git branch -D temp

# 方法2: 直接删除重建 ( Nuclear option )
Remove-Item -Recurse -Force .argus\backup-git
# 重启IDE后会重新创建
```

**预防**:
- 配置 `maxBackups: 100` (默认值)
- 定期清理旧备份 (可在设置中调整)

---

#### 问题3: 备份仓库损坏

**症状**:
```
[C] ❌ 备份仓库初始化失败
[C] ⚠️ 无法读取备份仓库config
```

**诊断**:
```powershell
# 检查.git目录完整性
cd .argus\backup-git
git fsck --full

# 检查config文件
Get-Content .git\config
```

**修复**:
```powershell
# 删除损坏的仓库，让IDE重建
Remove-Item -Recurse -Force .argus\backup-git
# 重启IDE
```

---

### 8.2 日志分析指南

**关键的日志标记**:

```
正常操作:
[C] AutoSave: 检测到 N 个文件变化
[C] 📁 创建AutoSave备份仓库: path
[C] ✅ 备份仓库初始化完成 (纯本地，无remote)
[C] ✅ AutoSave成功: 备份了 N 个文件

警告 (需关注):
[C] ⚠️ 备份仓库初始化失败
[C] ⚠️ 跳过敏感文件: filename
[C] ⚠️ 工作目录Git环境不安全，跳过auto-commit

错误 (必须处理):
[C] 🚨 危险！工作目录的Git remote指向主项目仓库
[C] 🚨 危险！备份目录可能是符号链接攻击！
[C] ❌ git init失败
[C] ❌ 复制文件失败: filename
```

---

## 9. 未来演进方向

### 9.1 短期改进 (1-2周)

- [ ] **前端UI**: 添加"本地历史"面板，可视化展示AutoSave时间线
- [ ] **回滚功能**: 一键恢复到任意备份点
- [ ] **差异对比**: 显示两个备份点之间的差异 (类似git diff)
- [ ] **智能排除**: 自动识别二进制文件和大文件，不纳入备份

### 9.2 中期优化 (1个月)

- [ ] **增量备份**: 只备份变更的部分，而非整个文件 (节省空间)
- [ ] **压缩存储**: 对历史备份进行gzip压缩
- [ ] **云端备份可选**: 用户可选择将备份同步到私有云 (如NAS)
- [ ] **多工作目录支持**: 同时管理多个项目的AutoSave

### 9.3 长期愿景 (3个月+)

- [ ] **协作备份**: 团队成员间共享备份 (端到端加密)
- [ ] **AI增强**: 利用AI分析备份历史，智能建议回滚点
- [ ] **灾难恢复**: 一键从任意备份点完整恢复整个工作环境
- [ ] **插件系统**: 允许第三方扩展备份逻辑

---

## 10. 附录

### 附录A: 术语表

| 术语 | 定义 |
|------|------|
| **IDE主仓库** | Argus IDE自身的源代码仓库 (argustek/Argus) |
| **用户项目仓库** | 用户在自己工作目录中的Git仓库 |
| **AutoSave备份仓库** | Argus在 `.argus/backup-git/` 创建的专用备份仓库 |
| **工作目录 (workDir)** | 用户选择作为项目根目录的文件夹 |
| **C监控** | Argus的后台守护进程，负责超时检测和AutoSave |
| **autoCommit** | C监控定期执行的自动Git commit操作 |
| **"女婿上床"** | 本文档记录的安全事故代号 |

---

### 附录B: 文件路径速查

```
E:\ArgusTek\Argus\                    # IDE项目根目录
├── app.go                             # GitClone安全检查在这里
├── internal\
│   ├── chat\manager.go               # ChatManager初始化C监控
│   └── monitor\c_monitor.go          # autoCommit()实现在这里
└── frontend\src\
    └── components\GitWindow.vue      # Git UI界面

E:\TempArgusTest\                      # 用户工作目录 (示例)
├── .git\                              # 用户项目仓库 (可选)
├── .argus\                            # IDE私有目录
│   ├── backup-git\                   # 🆕 AutoSave备份仓库
│   │   └── .git\                     # 独立的Git仓库!
│   ├── conversation.log
│   └── messages.json
├── config\
│   └── config.json                   # IDE运行配置
└── hello.go                           # 用户代码
```

---

### 附录C: 历史事件记录

| 日期 | 事件 | 影响 | 修复状态 |
|------|------|------|----------|
| 2026-05-09~28 | "女婿上床"事件持续 | 627个错误commit | ✅ 已解决 |
| 2026-05-28 | 发现问题并排查 | - | ✅ 已识别 |
| 2026-05-28 | 实施方案C (独立备份仓库) | 彻底根治 | ✅ 已部署 |
| 2026-05-28 | GitClone安全加固 | 预防再次发生 | ✅ 已部署 |
| 2026-05-28 | 撰写本文档 | 知识沉淀 | ✅ 完成 |

---

### 附录D: 相关文档索引

- [SE卡住问题修复总结](./SE-STUCK-FIX-SUMMARY.md)
- [全局任务追踪器设计](./GLOBAL-TASK-BAR-DESIGN.md) (待更新)
- [HTTP配置丢失问题修复](./HTTP-CONFIG-FIX.md) (待创建)

---

## 📝 文档维护信息

**维护者**: Argus Core Team  
**审核人**: [你的名字]  
**最后更新**: 2026-05-28 20:30  
**下次审查**: 2026-06-28 (一个月后)

**变更日志**:

| 版本 | 日期 | 作者 | 变更内容 |
|------|------|------|----------|
| v1.0 | 2026-05-28 | AI Assistant | 初版创建，完整记录事件和解决方案 |

---

## 🎯 核心要点回顾 (给忙碌的开发者)

```
✅ 记住这三件事就够了:

1️⃣ 三层空间必须分离:
   IDE代码 ↔ 用户工作 ↔ AutoSave备份
   各管各的，互不干扰

2️⃣ AutoSave只在 .argus/backup-git/ 里玩:
   不碰用户的.git
   不设任何remote
   不push到任何地方

3️⃣ 安全第一:
   GitClone要检查目标目录
   autoCommit要验证remote
   敏感文件绝不备份

违反以上任何一条 = "女婿又要上丈母娘的床" 😂
```

---

**🏆 文档结束**

> "好的架构不是没有bug，而是bug不会导致灾难性的后果。"  
> — Argus设计哲学

**如有疑问，请查阅本文档或联系维护团队。**