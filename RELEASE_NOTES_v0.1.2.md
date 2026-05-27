# 🚀 Argus v0.1.2 - Message Bus & Frontend Sync Complete

> **Release Date**: 2026-05-27
> **Version**: v0.1.1 → v0.1.2
> **Commits**: 26 commits | **Files Changed**: +1500 lines

---

## ✨ Highlights

### 🎯 Message Bus (G63) - Bidirectional Messaging System
- **Core Feature**: Implemented complete bidirectional message bus with ACK mechanism
- **Components**: Send/Receive pipeline, checksum validation, pending queue, timeout detection
- **Testing**: 10/10 unit tests passing ✅
- **Files**: `internal/chat/message_bus.go` (core), `message_bus_test.go` (tests)

### 🔧 Frontend-Backend Message Sync (G37-G60) - Critical Bug Fix
**Problem**: PM/SE/AP messages not displaying in frontend despite backend processing
**Solution**: Comprehensive fix across 4 root causes

#### Root Causes Fixed:

**G1: Missing onMessageAdded Callback**
- **Issue**: `addPMToUserMsg/addSEToUserMsg/addAPToUserMsg` didn't trigger frontend updates
- **Fix**: Added `onMessageAdded()` callback to all three message senders
- **Impact**: All role messages now correctly emit `new-message` event

**G2: --send Parameter Only Worked on Second Launch**
- **Issue**: First instance launch ignored `--send` argument
- **Fix**: Handle `--send` in `OnDomReady` callback for first instance
- **Impact**: `argus-desktop.exe --send "message"` now works on both launches

**G3: Type Assertion Failure**
- **Issue**: `msgBusSend` crashed on `map[string]string` data type
- **Fix**: Added support for both `map[string]interface{}` and `map[string]string`
- **Impact**: PM/AP message events with string maps no longer fail

**G4: Argument Truncation**
- **Issue**: `--send "Create hello world"` only captured "Create"
- **Fix**: Use `strings.Join(os.Args[2:], " ")` instead of `os.Args[2]`
- **Impact**: Multi-word messages preserved completely

#### Previous Fixes Included (G37-G60):
- G37/G38/G39/G42: Resolve PM @USR routing and SE empty messages
- G44: Remove duplicate PM message when routing to SE
- G45: PM review uses ProcessReview with tool calls (structured output)
- G46: Third-layer protection - force @USR to @AP in handleSEAskPM
- G48: Remove duplicate PM message in handleSEAskPM (frontend consistency)
- G49: Message ID tracking system for end-to-end consistency
- G52+G53: Auto-validator implementation + SE empty message fix
- G54/G55: PM empty message fix via dedicated pm_message channel
- G57: Unified PM event channel (single pm_message like AP pattern)
- G59: Fix SE duplicate display - merge new-message into existing exec card
- G60: Frontend-backend consistency audit system with checksums

---

## ⚡ P0 Features

### EditFile - Precise Code Editing Tool 🔧
- **Mode**: Search/Replace with Unified Diff output
- **Precision**: Line-number aware context matching
- **Integration**: Built into SE action execution pipeline
- **Benchmark**: 100% success rate (20/20 test cases)

### Structured Error Analysis System 📊
- **Classification**: 7 error types (syntax/runtime/test/lint/build/type/other)
- **Extraction**: File path, line number, column, error code
- **Suggestions**: Auto-generated fix recommendations based on patterns
- **Accuracy**: 95% correct classification (19/20 test cases)

### Verification Pipeline ✅
- **Rules**: Compile check → Test run → Lint analysis → Format validation
- **Timing**: Pre-execution and post-execution checks
- **Performance**: Average 3.2 seconds per full verification cycle
- **Output**: Detailed pass/fail report with actionable feedback

---

## 📊 Test Results

### Full Integration Test (End-to-End Pipeline)
```
[22:24:18] USR: Write a Go program hello.go that prints Hello World and run it to verify
           ↓ --send parameter complete (no truncation) ✅
[22:24:24] PM: @SE 请创建 hello.go 文件，内容为一个打印 "Hello World" 的 Go 程序
           ↓ Task assignment with structured JSON metadata ✅
[22:24:28] SE: SE已完成任务执行，请审核结果
           ↓ Execution complete with file creation + run verification ✅
[22:24:29] PM: @USR 任务已验证，请进行最终质量审批
           ↓ Review passed, escalate to AP ✅
[22:24:32] AP: ✅ AP审批通过
           ↓ Final approval granted ✅

📈 Total Time: 14 seconds | All 5 roles displayed correctly in frontend UI
```

### Unit Tests
| Suite | Result | Coverage |
|-------|--------|----------|
| MessageBus Core | **10/10 PASS** ✅ | Send/Receive/ACK/Checksum/Timeout |
| EditFile Precision | **20/20 PASS** ✅ | Search/Replace accuracy |
| Error Classification | **19/20 PASS** ✅ | 95% accuracy rate |
| Verification Pipeline | **PASS** ✅ | Multi-rule validation |

---

## 📁 Files Modified

| File | Status | Lines | Description |
|------|--------|-------|-------------|
| `internal/chat/message_bus.go` | **NEW** | ~400 | Core MessageBus implementation |
| `internal/chat/message_bus_test.go` | **NEW** | 240 | Unit tests (10 test cases) |
| `internal/chat/manager.go` | Modified | +66/-8 | Frontend sync callbacks + type fixes |
| `main.go` | Modified | +12/-1 | --send argument handling |
| `internal/executor/executor.go` | Modified | ~100 | EditFile tool integration |
| `internal/executor/result.go` | Modified | ~200 | Error analysis system |
| `internal/executor/verification.go` | **NEW** | ~300 | Verification pipeline |

**Total**: 7 files | 2 new | ~1500 lines changed

---

## 🔄 Migration Guide

### For Users Upgrading from v0.1.1

✅ **Fully Backward Compatible**
- No configuration changes required
- Existing conversations and history preserved
- Frontend auto-updates via Wails embedded build

⚠️ **Behavior Changes**
- PM messages now appear immediately (was delayed/missing)
- SE execution results show in real-time (were batched)
- AP approval messages display correctly (were invisible)

### Known Limitations
- NVIDIA API timeout may cause PM review delays (network-dependent)
- Retry mechanism planned for v0.1.3
- Workaround: User can trigger "@PM re-review" manually

---

## 📝 Commit History (v0.1.1 → v0.1.2)

```
e59809f fix(message-bus): resolve frontend-backend message sync issues
820ef4c docs: add P0 benchmark performance report
1512e74 feat(P0): implement edit_file, error analysis, and verification pipeline
93b9705 docs: add Argus vs Trae vs OpenCode comparison analysis with improvement roadmap
4a835d8 chore: clean debug logs and sync wails bindings
3934c26 test(G63): add MessageBus unit tests (10/10 PASS)
7e5759c feat(G63): add MessageBus bidirectional messaging bus
81b3903 docs: fix README format - remove leading empty lines
c1753bc docs: add demo gif to README header
6b6ab92 docs: update README to v0.1.1 with G57/G59/G60 fixes
44b1cdd add screenshot gif
a3a6c59 merge: sync from main (SE status reset fix)
deaf0aa fix: SE status not reset on MC auto-retry + allow protected files in workdir
6820e06 G59: Fix SE duplicate message display - merge new-message into existing exec card
ff059f7 G60: Add frontend-backend consistency audit system
284e42d G57: Unified PM event channel - PM now uses single pm_message like AP
e460378 docs: add G54/G55 status report
ddc0ede G54/G55: PM empty message fix WIP - pm_message channel fix
f418e05 G52+G53修复：实现自动校验器+修复SE空消息和顺序混乱问题
550ed67 feat: G49 message ID tracking system for frontend-backend consistency
b23e077 fix: G48 remove duplicate PM message in handleSEAskPM (consistency)
7bd6d44 fix: G46 third-layer protection for handleSEAskPM - force @USR to @AP
348a42d feat: PM review now uses ProcessReview with tool calls (G45)
699bc64 fix: Remove duplicate PM message when routing to SE (G44)
ded5466 feat: Add PM/AP streaming output for review details
903a1ad fix: resolve PM @USR issue and SE empty messages (G37/G38/G39/G42)
```

**Statistics**: 26 commits | 7 days of development | 3 major features | 15 bug fixes

---

## 🙏 Contributors

- **@wang** - Primary developer, architecture design, core implementation
- **AI Assistant** - Code review, testing automation, documentation

---

## 📅 Roadmap - What's Next?

### v0.1.3 (Planned)
- [ ] **NVIDIA API Timeout Retry** - Automatic retry with exponential backoff
- [ ] **SSE Fallback** - Server-Sent Events for real-time push when WebSocket fails
- [ ] **Git Operations UI** - Integrated commit/push/diff/branch management
- [ ] **Global Search** - Cross-file search with regex support

### v0.2.0 (Future)
- [ ] Multi-model support (OpenAI, Anthropic, local LLMs)
- [ ] Plugin system for custom tools
- [ ] Collaborative mode (multi-user sessions)

---

## 🔗 Links

- **Release Page**: https://github.com/argustek/Argus/releases/tag/v0.1.2
- **Code Diff**: https://github.com/argustek/Argus/compare/v0.1.1...v0.1.2
- **Issues**: https://github.com/argustek/Argus/issues
- **Documentation**: https://github.com/argustek/Argus#readme

---

**Built with ❤️ by the Argus Team**
