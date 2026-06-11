# 我做了一个 AI 编程助手，让它像老同事一样陪你写代码

> 从 Cursor 到 Copilot，AI 编程工具已经卷成红海了。但它们都有一个共同问题：**AI 只是一个"工具"，不是一个"搭档"**。

去年我开始做 **Argus**——一个基于 Wails (Go+Vue) 的桌面端 AI 编程助手。经过半年的迭代，v0.7.1 版本终于把一个想法变成了现实：**让 AI 像老同事一样，记住你、理解你、陪着你写代码**。

今天聊聊这个项目，以及我认为 AI 编程助手的下一个方向：**从"工具"到"搭档"**。

---

## 一、为什么现有 AI 编程助手不够用？

用过 Cursor、Copilot、Windsurf 的同学应该都有这种感觉：

| 痛点 | 现状 |
|------|------|
| **没有记忆** | 每次对话都是全新开局，AI 不认识你 |
| **没有角色分工** | 一个模型干所有事，PM/SE/QA 职责不清 |
| **没有时间感知** | 凌晨 3 点和上午 10 点的回复一模一样 |
| **错误处理弱** | 编译报错贴给 AI，它经常瞎改 |
| **不可扩展** | 想接入外部工具？没门 |

这些问题的本质是：**现有产品把 AI 当成了"更快的搜索引擎"，而不是"真正的协作伙伴"**。

## 二、Argus 的解法：三角色协作 + 记忆系统

### 2.1 PM-SE-AP 三角色模型

Argus 的核心架构是**三个 AI 角色各司其职**：

```
用户输入
   │
   ▼
┌─────────────┐
│  PM（项目经理） │ ← 理解需求、拆分任务、审核结果
│  性格：温和细致  │
└──────┬──────┘
       │ 任务分发
       ▼
┌─────────────┐
│  SE（软件工程师）│ ← 写代码、跑测试、修 bug
│  性格：直接高效  │
└──────┬──────┘
       │ 结果提交
       ▼
┌─────────────┐
│  AP（审批者）  │ ← 可选，质量把关
│  性格：严格挑剔  │
└──────────────┘
```

这不是 prompt engineering 的把戏，而是**真实的代码级隔离**：

- PM 和 SE 使用**不同的 system prompt**
- PM 和 SE 可以绑定**不同的 LLM**（比如 PM 用 GPT-4o 负责理解，SE 用 Claude 3.5 Sonnet 负责写代码）
- 每个角色的状态独立管理（busy/idle/reviewing）

### 2.2 记忆系统：从"新朋友"到"生死之交"

这是我最喜欢的功能。Argus 会记录你和它的**第一次交互时间**，然后随着时间推移，关系会自动升级：

```go
// social.go — 关系阶段判断
func determineRelationshipPhase(days int) (string, string) {
    switch {
    case days < 7:
        return "新朋友", "我们才认识几天，希望以后能成为好搭档！"
    case days < 30:
        return "老搭档", fmt.Sprintf("我们已经搭档%d天了，配合越来越默契~", days)
    case days < 90:
        return "老朋友", fmt.Sprintf("认识%d个月了，感谢一直以来的信任！", days/30)
    case days < 365:
        return "老战友", fmt.Sprintf("一起奋斗了%d个月，算是老战友了！", days/30)
    default:
        return "生死之交", "🎉 哇！我们已经认识N年N个月了！..."
    }
}
```

**实际效果**：

| 场景 | 普通AI | Argus |
|------|--------|-------|
| 你隔了一周回来 | "有什么可以帮您？" | "一周没见了！最近在忙什么大项目吗？期待你的分享~ 🚀" |
| 晚上 10 点还在加班 | 无感知 | "天呐，你已经工作12小时+！赶紧下班休息吧，身体第一！🙏" |
| 认识满一年 | 同上 | "🎉 我们已经认识1年1个月了！从陌生人到老朋友..." |

这套系统的实现其实不复杂：

1. **State 持久化** — `FirstInteractionTime` + `LastInteractionTime` 存在本地 JSON
2. **时间上下文注入** — 每次 PM 处理消息时，自动拼接时间感知信息到 prompt
3. **社交触发器** — 久违检测(>24h)、早安问候、下班关心、节日祝福、随机寒暄

```go
// 注入到 PM 的 System Prompt 中
fmt.Sprintf(`## ⏰ 时间感知（重要！）
当前时间: %s
距上次交互: %s
关系阶段: %s
今天工作了%.1f小时
...
- 保持自然，像真同事/老朋友一样
- 关系越深（%s），表达可以越真诚`,
    ctx.CurrentTime, ctx.TimeSinceLast,
    ctx.RelationshipPhase, ctx.TodayWorkHours, ctx.RelationshipPhase,
)
```

### 2.3 结构化错误处理：不再瞎改代码

大多数 AI 编程助手处理错误的方式是：**把错误信息贴过去，让 AI 自己猜**。

Argus 做得更进一步——先解析错误，再交给 AI：

```go
// ErrorParser — 8 类错误自动分类
type ErrorType string
const (
    ErrorSyntax     ErrorType = "SYNTAX"      // 语法错误
    ErrorCompile    ErrorType = "COMPILE"      // 编译错误
    ErrorRuntime    ErrorType = "RUNTIME"      // 运行时错误
    ErrorImport     ErrorType = "IMPORT"       // 导入错误
    ErrorTestFail   ErrorType = "TEST_FAILURE" // 测试失败
    // ...
)

// 三级分类：瞬态 / 可修复 / 永久
func ClassifyError(err *ParsedError) ErrorClass {
    if isTransient(err) { return Transient }    // 重试即可（网络超时等）
    if isFixable(err)   { return Fixable }      // AI 可以修复（语法错误等）
    return Permanent                           // 需要人工介入（逻辑错误）
}

// 格式化后发给 SE
func FormatErrorForSE(err *ParsedError) string {
    return fmt.Sprintf("❌ [%s] %s:%d:%d\n%s\n---\n%s",
        err.Type, err.File, err.Line, err.Col, err.Message, err.CodeContext)
}
```

**效果对比**：

```
普通 AI：
> 用户：编译报错了 [粘贴 50 行错误输出]
> AI：让我看看...可能是 xxx 问题，你试试改成 yyy
> 用户：还是不行
> AI：那试试 zzz？
> 用户：...

Argus：
> 用户：编译报错了
> SE：[自动解析] ❌ [SYNTAX] main.go:23:12 unexpected comma, expected ]
> SE：[自动修复] 检测到 syntax error，正在定位...
> SE：✅ 已修复：移除了多余的逗号（第23行）
```

### 2.4 MCP 协议支持：无限扩展可能

v0.7.1 加入了 **MCP (Model Context Protocol)** 支持，这是 Anthropic 提出的开放协议，允许 AI 动态接入外部工具：

```yaml
# config.yaml
mcp_servers:
  - name: "filesystem"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
    
  - name: "github"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
```

Argus 的 MCP 实现：

- **JSON-RPC 2.0 stdio Client** — 与 MCP Server 通信
- **Manager** — 管理多个 Server 生命周期
- **5 个 REST API** — 动态增删查 Server 和工具
- **SE 工具桥接** — MCP 工具自动变成 SE 可调用的 action

这意味着你可以**热插拔任何 MCP 兼容工具**，不需要改 Argus 代码。

## 三、技术栈选择：为什么是 Go + Vue？

| 层 | 技术选型 | 理由 |
|----|---------|------|
| 后端 | Go | 工具链生态好（LSP client、go/parser、go/ast），并发简单 |
| 前端 | Vue 3 + TypeScript | 组件化，终端渲染方便 |
| 桌面框架 | Wails | Go 原生性能 + Web 前端体验，打包体积小（~15MB） |
| 存储 | SQLite | 零配置，单文件，适合桌面应用 |
| AI 接口 | OpenAI 兼容格式 | 支持 GPT/Claude/DeepSeek/Ollama 等 |

Wails 这个框架值得单独说一下。它比 Electron 轻量太多：

```
Electron 应用：~150MB 打包体积，~200MB 内存占用
Wails 应用：   ~15MB  打包体积，~30MB  内存占用
```

而且 Go 后端可以直接调用操作系统能力（shell、文件系统、进程管理），不需要通过 IPC 桥接。

## 四、实际使用场景

### 场景 1：Hello World 开发

```
用户：帮我创建一个 hello.go，输出 Hello World

PM：收到需求，拆分任务：
  ① 创建 hello.go 文件
  ② 写入 Hello World 代码
  ③ 运行验证

SE：执行中...
  ✅ 创建文件: E:\test\hello.go
  ✅ 写入代码: package main + func main() + fmt.Println
  ✅ 运行成功: Hello World

PM：任务完成！代码已运行通过 ✅
```

### 场景 2：编译错误自动修复

```
用户：帮我加个函数，计算两个数的和

SE：[写入代码]
SE：[运行 go build]
SE：❌ [COMPILE] calc.go:15:12 undefined: Println

ErrorParser 自动分类 → Fixable → 触发 Auto-Fix
SE：✅ 已修复：Println → fmt.Println
SE：[重新编译] ✅ 通过
```

### 场景 3：跨文件重构

```
用户：把 getUserName 改成 getUserName

SE：[调用 LSP find_references]
SE：找到 3 个引用点：
  - user.go:23  (定义)
  - handler.go:45 (调用)
  - test.go:12  (测试)

SE：[调用 LSP rename_symbol]
SE：✅ 3 处已同步重命名
PM：✅ 重构完成，无遗漏
```

## 五、当前能力和下一步计划

### v0.7.1 能力矩阵

| 维度 | 得分 | 说明 |
|------|------|------|
| 角色协作 | 9.0 | PM-SE-AP 三角色完整实现 |
| 项目管理 | 8.5 | 任务拆分/进度跟踪/审核流程 |
| 文档处理 | 8.0 | PDF/Word/Excel 读取和摘要 |
| 代码生成 | 7.2 | 多语言支持，AST 感知编辑 |
| 工具链完善度 | 7.5 | LSP 全套 + 测试解析 + MCP |
| 错误处理机制 | 7.0 | ErrorParser 8类分类 + Auto-Fix |
| 调试与测试 | 6.5 | JSON 测试解析 + Tab 补全终端 |
| 自我纠错 | 5.5 | 有 AP 审核，但还不够主动 |
| 架构可扩展性 | 8.5 | MCP 协议 + 插件化设计 |

**加权平均：7.4/10**（v0.7.0 时为 6.5）

### 下一步方向

1. **真正的 Debugger** — 断点/单步/变量检查（唯一剩余的 P0 差距）
2. **更强的自我纠错** — AP 角色从被动审核变为主动巡检
3. **多项目管理** — 同时维护多个项目的上下文
4. **团队模式** — 多人共享同一个 Argus 实例

## 六、开源地址

GitHub: https://github.com/argustek/Argus

欢迎 Star / Issue / PR。如果你对以下方向感兴趣，特别欢迎贡献：

- **前端体验优化**（Vue 3 + TypeScript）
- **更多语言的 AST 编辑**（Python/Rust/TypeScript）
- **MCP Server 开发**（接入更多外部工具）
- **Prompt 工程**（优化 PM/SE/AP 的 system prompt）

---

## 写在最后

AI 编程助手的竞争远未结束。但我相信，**下一个突破口不是"更聪明的模型"，而是"更有温度的产品"**。

当一个 AI 能记住你们一起熬过的夜、修过的 bug、庆祝过的里程碑——它就不再是一个工具，而是一个**真正的工作搭档**。

这正是 Argus 在做的事情。

---

*如果这篇文章对你有启发，欢迎点赞收藏。也欢迎在评论区分享你对 AI 编程助手的期待和建议！*
