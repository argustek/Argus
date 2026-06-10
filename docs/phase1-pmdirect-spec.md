# 阶段1：项目分级 + pmDirectExecute 实施规范

> **版本**: v0.8.0-Phase1
> **日期**: 2026-06-10
> **状态**: 待实施
> **前置文档**: `docs/智能化分级.md`（总蓝图）

---

## 一、目标

让 Featherweight 级别的任务走 **pmDirectExecute** 路径：PM 一次调用搞定，戴 PM 帽子，不走 SE/Review/AP。

**不做的事**：
- ❌ 文档分级 L0-L3
- ❌ CHANGELOG / log_change 工具函数
- ❌ C 监控模块
- ❌ Skills 目录结构
- ❌ Lightweight/Medium/Heavy 流程改动

---

## 二、项目自动分级

### 2.1 分级方式（两种）

| 方式 | 触发条件 | 优先级 |
|------|----------|--------|
| **用户指定** | 消息包含 `/level featherweight\|lightweight\|medium\|heavy` | **高** — 用户说了算 |
| **PM 判断** | 用户没指定，PM 分析时根据标准自行判断 | 低 — 兜底 |

> 用户有优先权。用户指定了 level，PM 不能覆盖。

### 2.2 分级标准

| 级别 | 标准 | 流程 |
|------|------|------|
| **Featherweight** ⚡ | 单文件 / <100行 / 无依赖 | pmDirectExecute（PM直执） |
| **Lightweight** ⚡ | 2-5文件 / <500行 / 单一功能 | PM → SE → AP 快速验证 |
| **Medium** | 多模块 / <5000行 / 有内部依赖 | PM → SE → Review(循环) → AP → **PM总结** |
| **Heavy** | 大型项目 / 多团队协作 / 长周期 | 全套 + K Tool 影响分析 → AP → **PM总结** |

### 2.3 /level 命令格式

```
用户输入示例：
  "创建 hello world"                    → PM 自行判断
  "/level featherweight 创建 hello"     → 强制 Featherweight
  "/level heavy 重构数据库层"           → 强制 Heavy
```

解析位置：`Process()` 函数入口处，在调用 PM 之前拦截。

---

## 三、流程架构（高速路 + 分流器）

### 3.1 架构图

```
用户消息
   ↓
[解析 /level 命令] ── 有 → userLevel = 指定值
   ↓ 无
[PM 分析] ← 用修改后的 PM prompt（带 SE 全部能力）
   ↓
[PM 返回] level 标记 + actions（如果有）
   ↓
┌──────── 分流器 ────────┐
│                         │
│  isFeatherweight?       │
│    ├─ YES → pmDirectExecute  (新增)
│    ├─ NO  → isLightweight?  │
│    │        ├─ YES → PM→SE→AP      (现有逻辑)
│    │        └─ NO  → 完整流程        (现有逻辑)
│    │              PM→SE→Review(循环)→AP→PM总结
└─────────────────────────┘
   ↓
[结束]
```

**关键原则**：
- Process() 是**唯一入口**
- 分流器在 Process() **内部**实现（if/else），不是单独的函数分支
- 不允许"国道"——所有任务走同一条高速路，中间分流跳站

---

## 四、pmDirectExecute 详细规范（核心）

### 4.1 定义

PM 在自己工位上，用 SE 的工具和能力，一次性完成任务。**戴 PM 帽子，不换 SE 帽子。**

### 4.2 Prompt 设计

**修改 PM prompt**，赋予其全部 SE 能力：

```markdown
你现在是 PM（项目经理），同时具备 SE（软件工程师）的全部执行能力。

## 你的能力
- 任务分析和拆解（PM能力）
- 直接编写代码和执行命令（SE能力）
- 使用所有工具：read_file, write_file, exec, edit_file, glob 等

## 当任务为轻量级时（单文件/<100行/无依赖）
你应该：
1. 分析需求
2. 直接生成代码（write_file）
3. 执行验证（exec: go run xxx.go）
4. 向用户汇报结果

要求：在一次响应中返回完整的 actions（write_file + exec）和结果汇报文本。
不要分多次调用，一口气干完。

## 结果汇报
- PM 在**同一次 LLM 响应的 Content 字段**中包含结果汇报文本
- exec 命令的输出也直接展示给用户
- 不额外调用 LLM 生成 summary（不换帽子、不换工位）
```

### 4.3 执行流程（精确到每一步）

```
Step 1: 调用 LLM（第1次）
  - prompt: 修改后的 PM prompt（带 SE 能力）
  - input: 用户原始消息
  - output: msg.ToolCalls (actions) + msg.Content (汇报文本/summary)

Step 2: 解析 actions
  - 如果有 ToolCalls → 提取 actions 数组
  - 如果没有 → 直接把 Content 发给用户，结束

Step 3: 执行 actions（用 executeActions，executor="pm"）
  - executor 参数传 "pm"（不是 "se"！）
  - emit 时 source 为 "pm_to_user"（不是 "se_to_user"）

Step 4: 检查执行结果 + 汇报
  - 成功 → 汇报给用户（PM 的 Content 文本 + exec 输出），结束 ✅
  - 失败 → 进入 Step 5 重试

Step 5: 错误重试（最多 3 次，共可尝试第2/3/4次 LLM 调用）
  - 第N次重试: 调用 LLM 带上错误信息
  - 要求修复并重新返回 actions + 汇报文本
  - 执行修复后的 actions
  - 任一重试成功 → 汇报给用户 ✅
  - 3次全失败 → 报错给用户 ❌，结束
```

### 4.4 LLM 调用次数限制

| 场景 | 最少 | 最多 |
|------|------|------|
| 正常情况 | **1次** | 1次 |
| 出错重试 | 2-4次 | **4次（1+3重试）** |
| 绝不允许 | — | >4次 |

### 4.5 结果汇报

- **PM 的 Content 文本**（第1次 LLM 响应自带）作为 summary 展示
- **exec 命令输出**也直接展示给用户
- **不额外调用 LLM 生成 summary**
- 格式：`@USR ⚡ [PM汇报文本]` + `[exec输出（如有）]`
- **不换帽子、不换工位** — 全程 PM 角色

### 4.6 Featherweight 不做的事

| 不做 | 原因 |
|------|------|
| ❌ 额外调 LLM 生成 summary | PM Content 文本就是 summary，不换工位 |
| ❌ Empty actions 无条件重试 | 提示词写对就不会空 |
| ❌ Self-Fix 循环（>3次） | 最多重试 3 次 |
| ❌ 写文档（L0-L3） | Featherweight 不启用文档 |
| ❌ 写 CHANGELOG | Featherweight 不记录变更 |
| ❌ 单独的 PM 总结步骤 | 汇报文本已在 LLM 响应中 |
| ❌ Review / AP 审核 | 直接跳过 |

---

## 五、角色显示规范

### 5.1 关键点

pmDirectExecute 全程必须显示 **PM 角色**，不能出现 SE。

### 5.2 需要改的位置

| 位置 | 当前值 | 改后值 | 说明 |
|------|--------|--------|------|
| `executeActions(executor)` | 硬编码 `"se"` | 动态传入 `"pm"` | argus.go |
| `emit(source)` | 硬编码 `"se_to_pm"` | 动态 `executor+"_to_user"` | argus.go |
| Bridge OnActionEvent | 硬编码发送者 `"se"` | 从 data 取 executor | bridge.go |
| Bridge roleFromSource | 按 source 判断 | 新增 pm_to_user → "pm" | bridge.go |
| conversation.log | 显示角色 | 显示 "PM:" 而非 "SE:" | bridge.go |

---

## 六、现有流程保持不变

以下流程**阶段1不动**，保持现有逻辑：

- ✅ Lightweight: PM → SE → AP（快速验证）
- ✅ Medium: PM → SE → Review(循环) → AP → PM总结
- ✅ Heavy: 全套 + K Tool + PM总结
- ✅ PM 总结生成（Medium/Heavy 用）
- ✅ AP 审核（Medium/Heavy 用）

---

## 七、验证标准

阶段1完成后，必须通过以下测试：

### TEST1: Hello World（Featherweight）
```
输入: "Create a hello world Go program"
预期:
  - 日志显示 [Core:Level] ⚡ Featherweight (PM直执)
  - conversation.log 显示 "PM:" 不是 "SE:"
  - 前端显示 PM 角色
  - hello.go 文件已创建且可运行
  - 输出 Hello World 结果
  - LLM 总调用次数 ≤ 4次（正常1次 + 最多3次重试）
```

### TEST2: Fibonacci（Featherweight）
```
输入: "Create a Go program that calculates Fibonacci(10)"
预期: 同上，fib.go 可运行输出 55
```

### TEST3: 自定义任务（Featherweight）
```
输入: "Create counter.go counting 1 to 5"
预期: 同上，输出 1 2 3 4 5
```

### TEST4: 非 Featherweight 不受影响
```
输入: "/level medium 重构数据库连接池"
预期: 走完整 Medium 流程（PM→SE→Review→AP→总结）
```

---

## 八、文件改动清单

| 文件 | 改动内容 |
|------|----------|
| `internal/ai/pm_prompt.go` | 修改 PM prompt，加入 SE 全部能力和分级判断指令 |
| `internal/core/argus.go` | Process() 加分流器 + 新增 pmDirectExecute() + executeActions 加 executor 参数 |
| `internal/chat/bridge.go` | OnActionEvent 动态获取 executor + roleFromSource 支持 PM |

**共 3 个文件。**

---

## 九、风险和约束

| 风险 | 应对 |
|------|------|
| PM prompt 膨胀 | 控制在 ~80 行以内，规则精简 |
| LLM 返回空 actions | 提示词明确要求"必须返回 write_file + exec"，最多重试 1 次 |
| 角色显示错乱 | executor 参数贯穿全链路，不在任何地方硬编码 "se" |
| 分流器误判 | PM 自行判断 level + 用户可 /level 覆盖，双重保障 |
