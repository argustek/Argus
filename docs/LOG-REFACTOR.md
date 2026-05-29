# 日志目录重构文档

**日期**: 2026-05-29  
**版本**: v1.0  
**作者**: Argus开发团队  

---

## 1. 重构背景

### 1.1 问题

Argus IDE的调试日志（conversation.log、route.log、dingtalk.log）原来存放在**用户工作目录**（workDir）的 `.argus/` 下。这导致：

1. **语义混乱**：调试日志是IDE开发者用的，不是用户项目数据，不应该跟项目走
2. **目录污染**：用户的项目`.argus/`里混入了IDE调试数据
3. **数据归属不清**：工作目录随时可能被删除/切换，调试日志应该跟随IDE持久保存

### 1.2 核心原则

> **工作目录是可以随时删除/切换的，所以IDE配置和调试日志不应该进去！**

工作目录（workDir）只存放**项目运行时数据**（可丢可重建的）：
- `state.json` → 项目状态
- `board.json` → 看板
- `task_memory.json` → 任务记忆
- `memory.db` → 对话记忆数据库
- `messages.json` → 对话历史（前端恢复用）
- `env_memory.json` → 环境记忆

---

## 2. 重构方案

### 2.1 三层目录架构

```
Argus IDE 系统目录 (E:\ArgusTek\Argus\)    ← 持久不变（丈母娘家）
├── config/                                 ← IDE全局配置
│   ├── config.json                         ← API Key、workDir等
│   └── dingtalk.example.json               ← 钉钉配置模板
├── logs/                                   ← 🆕 调试日志（IDE级别）
│   ├── conversation.log                    ← 对话调试日志（原workDir/.argus/）
│   ├── route.log                           ← 路由调试日志（原workDir/.argus/）
│   ├── dingtalk.log                        ← 钉钉调试日志（原workDir/.argus/）
│   └── debug_events.log                   ← 前端调试事件（原config/）
├── argus.log                               ← IDE运行主日志（不变）
└── (源代码、build等)

用户工作目录 (E:\TempArgusTest\)             ← 可随时切换/删除（女婿家）
├── (用户代码、git...)
└── .argus/                                 ← 项目运行时数据（可丢）
    ├── state.json                           ← 项目状态
    ├── board.json                           ← 看板
    ├── task_memory.json                     ← 任务记忆
    ├── memory.db                            ← 对话记忆数据库
    ├── messages.json                        ← 对话历史（前端恢复用）
    ├── env_memory.json                     ← 环境记忆
    ├── command_policy.json                 ← 命令策略
    ├── permission_config.json              ← 权限配置
    ├── decision_config.json                ← 决策配置
    └── resets/                              ← 重置记录
```

### 2.2 修改清单

| 文件 | 修改内容 | 原路径 | 新路径 |
|------|---------|--------|--------|
| `manager.go` | `writeConversationLog` | `workDir/.argus/conversation.log` | `configDir/../logs/conversation.log` |
| `manager.go` | `writeRouteLog` | `workDir/.argus/route.log` | `configDir/../logs/route.log` |
| `manager.go` | `startConversationMonitor` | `workDir/.argus/conversation.log` | `configDir/../logs/conversation.log` |
| `dingtalk/stream.go` | `logToFile` | `.argus/dingtalk.log` | `logDir/dingtalk.log` |
| `manager.go` | `NewManager` 参数 | `(config, workDir)` | `(config, workDir, configDir)` |
| `app.go` | `NewManager` 调用 | `NewManager(config, projectDir)` | `NewManager(config, projectDir, getConfigDir())` |
| `cmd/argus/main.go` | CLI NewManager | `NewManager(config, workDir)` | `NewManager(config, workDir, ".")` |
| `*_test.go` | 测试 NewManager | `NewManager(config, tmpDir)` | `NewManager(config, tmpDir, tmpDir)` |

---

## 3. 开发者指南

### 3.1 查看调试日志

```powershell
# 对话调试日志（PM/SE/AP/USR对话 + SYS_C + DEBUG）
Get-Content E:\ArgusTek\Argus\logs\conversation.log -Tail 30

# 路由调试日志（消息路由、SE任务来源等）
Get-Content E:\ArgusTek\Argus\logs\route.log -Tail 30

# 钉钉调试日志
Get-Content E:\ArgusTek\Argus\logs\dingtalk.log -Tail 30

# IDE运行主日志
Get-Content E:\ArgusTek\Argus\argus.log -Tail 30
```

### 3.2 文件归属原则

| 类别 | 存放位置 | 生命周期 | 删除影响 |
|------|----------|---------|---------|
| IDE配置 | `config/` | 永久 | 丢失API Key等配置 |
| 调试日志 | `logs/` | 可定期清理 | 无影响（自动创建） |
| 项目数据 | `workDir/.argus/` | 跟随项目 | 丢失项目状态（可重建） |

---

## 4. 注意事项

1. **`conversation.log` 不是对话历史**：它是调试日志，包含SYS_C/DEBUG等调试信息。真正的对话历史在 `memory.db` 和 `messages.json` 中。
2. **`.gitignore` 已排除 `.argus/`**：工作目录的 `.argus/` 不会被提交到git。
3. **`logs/` 目录也应加入 `.gitignore`**：调试日志不应提交到版本控制。