# SE Function Calling 重构规划

**日期**: 2026-06-02  
**版本**: v1.0  
**作者**: Argus开发团队  

---

## 1. 重构背景

### 1.1 现状

SE（软件工程师）通过 OpenAI 兼容 API 调用 LLM 执行编码任务。当前架构：

```
用户 → PM 下任务 → SE 调用 AI (ChatStream)
                         │
                         ▼
               AI 输出原始文本字符串:
               {"actions":[{"type":"write_file","path":"main.go","content":"package main..."}]}
                         │
                         ▼
               Go 代码 extractActions() 解析 JSON 字符串
                         │
                         ▼
               ❌ JSON 格式错误 → fixMalformedJSON() 正则补丁修复
```

### 1.2 核心问题

**要求 LLM 自行拼装 JSON 字符串不可靠。** 这导致：

| 问题类别 | 具体表现 | 影响 |
|----------|---------|------|
| **路径错误** | `E:\\TempArgusTestmain.go`（分隔符丢失） | write_file 失败 |
| **路径错误** | `:\\TempArgusTest\\main.go`（盘符丢失） | mkdir 语法错误 |
| **JSON 格式** | `"type"write_file"`（缺少冒号） | 解析失败 |
| **JSON 格式** | `{"":"hello.go"}`（key 名丢失） | 全部策略失败 |
| **Go 代码错误** | `fmt.Println"Hello"`（缺括号+引号） | 编译失败 |
| **截断** | 字符串未闭合 `"Hello World\n}` | JSON 非法 |
| **模型无关性** | DeepSeek、GLM 均出现以上问题 | 换模型不解决 |

### 1.3 根本原因

**架构缺陷**：SE 使用 `ChatStream`（原始文本流），AI 只能通过拼接字符串来输出结构化数据（JSON actions）。这与 PM/AP 已采用的 `ChatWithTools`（function calling）形成断层。

| 角色 | 调用方式 | 结构化方式 | 可靠性 |
|------|---------|-----------|--------|
| PM | `ChatWithTools` | Function Calling API | ✅ 框架保证 JSON |
| AP | `ChatWithTools` | Function Calling API | ✅ 框架保证 JSON |
| SE | `ChatStream` | 原始文本 → 正则解析 | ❌ AI 手动拼 JSON |

### 1.4 现有补丁代码统计

当前为修复 JSON 格式问题累积了大量防御性代码：

| 函数 | 行数 | 用途 |
|------|------|------|
| `extractActions()` | ~90 | 3 层策略容错（JSON → Code Block → 放弃） |
| `extractActionsFromJSON()` | ~80 | 花括号配对解析 |
| `fixMalformedJSON()` | ~100 | 10+ 条正则补丁：路径粘连、type 缺失、冒号丢失... |
| `fixActionTypes()` | ~50 | type 字段推断修复 |
| `fixJSONNewlines()` | ~40 | 换行符转义修复 |
| **合计** | **~360 行** | 用于修复 AI JSON 输出的补丁代码 |

---

## 2. 目标架构

### 2.1 设计原则

> **让框架保证结构化，不让 AI 拼字符串。**

### 2.2 新架构

```
用户 → PM 下任务 → SE 调用 AI (ChatWithTools + SETools)
                         │
                         ▼
               AI 返回 ToolCall（由 API 协议保证格式）:
               {name:"write_file", arguments:{"path":"main.go","content":"package main..."}}
                         │
                         ▼
               Go 框架直接映射 ToolCall → SEAction
                         │
                         ▼
               ✅ 零 JSON 解析错误，零正则补丁
```

### 2.3 SE 工具定义 (SETools)

| 工具名 | 描述 | 参数 |
|--------|------|------|
| `write_file` | 创建或覆盖文件 | `path`(string), `content`(string) |
| `exec` | 执行命令 | `command`(string) |
| `read_file` | 读取文件内容 | `path`(string) |
| `edit_file` | 精确编辑现有文件 | `path`(string), `old_str`(string), `new_str`(string) |
| `list_files` | 列出工作目录文件 | 无参数 |
| `search_files` | 搜索文件内容 | `pattern`(string), `file_pattern`(string) |
| `git_operation` | Git 版本控制 | `git_action`(string), `git_message`(string), `git_args`([]string) |
| `run_tests` | 运行测试 | `test_pattern`(string) |
| `complete_task` | 标记任务完成 | `status`(string), `summary`(string), `files`([]string) |

### 2.4 映射逻辑

```go
// ToolCall → SEAction 映射（新增）
func toolCallToSEAction(tc ToolCall) SEAction {
    return SEAction{
        Type:    tc.Function.Name,        // "write_file" / "exec" / ...
        Path:    args["path"],            // 框架保证类型正确
        Content: args["content"],
        Command: args["command"],
        // ... 其他字段直接映射
    }
}
```

### 2.5 与现有 ESEaction 接口兼容

`executeSEActions()` 接口不变，SE 输出的 `[]SEAction` 结构不变。**仅改变 SE 获取 actions 的方式**（从 JSON 字符串解析 → 从 ToolCall 映射）。

---

## 3. 实施计划

### 3.1 阶段一：新增（不影响现有功能）

| 步骤 | 文件 | 内容 |
|------|------|------|
| 1 | `se_prompt.go` | 定义 `SETools` 工具列表 |
| 2 | `se_prompt.go` | 新增 `ProcessTaskWithTools()` 方法 |
| 3 | `se_prompt.go` | 新增 `toolCallToSEAction()` 映射函数 |
| 4 | `se_prompt.go` | 更新 SE Prompt（去掉 JSON 格式规则，改为工具调用说明） |

### 3.2 阶段二：切换

| 步骤 | 文件 | 内容 |
|------|------|------|
| 5 | `manager.go` | 配置项控制 `seFunctionCalling` 开关 |
| 6 | `manager.go` | `startSETaskWithFrom` 中根据开关选择调用路径 |
| 7 | — | 测试验证 DeepSeek/GLM/GPT 多模型 |

### 3.3 阶段三：清理（稳定后）

| 步骤 | 文件 | 内容 |
|------|------|------|
| 8 | `se_prompt.go` | 删除 `fixMalformedJSON()`（~100行） |
| 9 | `se_prompt.go` | 删除 `extractActionsFromJSON()`（~80行） |
| 10 | `se_prompt.go` | 删除 `extractActionsFromCodeBlocks()`（~20行） |
| 11 | `se_prompt.go` | 删除 `fixJSONNewlines()`（~40行） |
| 12 | `se_prompt.go` | 删除 `fixActionTypes()` 冗余逻辑 |
| 13 | `se_prompt.go` | 清理 SE Prompt 中的 JSON 格式规则 |
| — | **合计** | **-360 行死代码** |

---

## 4. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 某些模型不支持 function calling | 低 | 中 | 降级回 ChatStream（保留旧逻辑不删，阶段三才清理） |
| ToolCall 参数格式差异 | 低 | 中 | API 协议统一（OpenAI 兼容），参数映射层处理 |
| 流式输出变非流式 | 中 | 低 | ChatWithTools 非流式，SE 输出延迟稍增（~1-3秒），可接受 |
| PM 侧已有成熟先例 | — | — | PM `ChatWithTools` + `PMTools` 已稳定运行，SE 直接复用模式 |

---

## 5. 收益

| 维度 | 改善 |
|------|------|
| **代码量** | 净减 ~300 行（新增 ~80，删除 ~360，净 -280） |
| **可靠性** | JSON 由框架保证，不再依赖 AI 拼字符串的质量 |
| **模型兼容性** | 任何 OpenAI 兼容模型均可用，不受 JSON 拼装能力限制 |
| **可维护性** | 去掉 10+ 条正则补丁，逻辑清晰 |
| **一致性** | SE/PM/AP 三者统一使用 Function Calling 架构 |

---

## 6. 决策记录

| 决策 | 结论 | 理由 |
|------|------|------|
| 新旧并存 | 阶段一新增，阶段二开关切换，阶段三删旧 | 渐进式，可随时回退 |
| SE Prompt 改动 | 去掉 JSON 格式规则，加上工具调用说明 | 参考 PM Prompt 中工具描述风格 |
| 路径策略 | 工具参数中的路径使用相对路径 | 执行器自动拼接 workDir，避免转义问题 |

---

## 7. 附录

### 7.1 当前 SE JSON 解析错误日志采样

```
[15:22:08] write_file E:\\TempArgusTestmain.go     ← 分隔符丢失
[15:32:02] write_file :\\TempArgusTest\\main.go     ← 盘符丢失
[17:39:41] EXTRACT-ACTIONS-FAILED (缺首个{)
[18:16:08] "actions      "type": "exec"            ← 引号粘连
[18:56:07] "type":"execcommand":"go run hello.go"   ← type+command粘连
[19:04:55] "type":"write_file",".go","content":...  ← path字段名丢失
```

### 7.2 PM 的 Function Calling 参考实现

`internal/ai/pm_prompt.go:284-383` — `PMTools` 定义 + `ChatWithTools` 调用，SE 改造直接复用此模式。
