# Argus 当前状态文档（v0.8.0）

**最后更新：** 2026-06-10

---

## 1. 核心功能状态

| 功能 | 状态 | 说明 |
|------|------|------|
| **API 配置热重载** | ✅ 已修复 | 在共享模式下，切换模型后无需重启，客户端实例正确重建。 |
| **Featherweight → short** | ✅ 已替换 | 所有 `🪶` 已替换为 `[short]`，避免 emoji 兼容问题。 |
| **PM 直执模式** | ✅ 已实现 | 单文件/轻量任务由 PM 直接执行，跳过 SE/AP 流程。 |
| **MessageBus LOST 修复** | ✅ 已修复 | 移除 TokenMonitor 的 EventsOff，确保 token_stats/context_built 事件不丢失。 |
| **项目级别指示器** | ⚠️ 未实现 | 前端 TopBar 无级别显示，当前仅后端自动分类。 |
| **Idle 状态指示** | ⚠️ 部分失效 | 前端仍显示 `idle`/`busy`，但后端已不再主动推送完整状态流，依赖事件驱动。 |

---

## 2. 代码变更摘要

### 已提交变更

- `internal/core/argus.go`：替换 `🪶` → `[short]`，保留内部 `Featherweight` 逻辑
- `internal/ai/pm_prompt.go`：替换 `🪶` → `[short]`
- `docs/phase1-pmdirect-spec.md`：替换 `🪶` → `[short]`
- `docs/RELEASE_NOTES_v0.7.3.md`：已发布 v0.7.3
- `docs/RELEASE_NOTES_v0.8.0.md`：已发布 v0.8.0

### 待处理

- 前端 TopBar 添加 `[short]`/`[normal]`/`[full]` 指示器（未实现）
- 重构 Idle 状态推送机制（可选）

---

## 3. 当前版本

- **最新 Tag**：`v0.8.0`
- **最新 Commit**：`5975593`
- **最新 Release**：https://github.com/argustek/Argus/releases/tag/v0.8.0

---

## 4. 下一步建议

1. **优先级高**：在 TopBar 添加项目级别指示器（`[short]` 等）
2. **可选**：清理废弃的 `idle`/`busy` 状态逻辑
3. **文档**：更新 `docs/智能化分级.md`，统一使用 `short/normal/full`

> *注：所有核心 Bug 已修复，当前为功能稳定版。新增功能建议按需迭代。*