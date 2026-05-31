# Argus v0.6.0 Release Notes

**Release Date:** 2026-05-31  
**Version:** v0.6.0  
**Previous Version:** v0.1.2

---

## 🎉 Major Highlights

### ✨ V2 Architecture Refactoring (Complete)
- **Full Pipeline Implementation**: PM Analysis → SE Execution → PM Code Review → AP OA Approval
- **LabVIEW-style State Sync Architecture**: Unified `RoleState` structure with MessageBus-driven state push
- **Shared Memory System**: Single-source data flow eliminating message passing complexity
- **Code Reduction**: ~80% reduction (from ~4300 lines to ~850 lines in core modules)

### 🛡️ Robustness Improvements
- **SE JSON Parsing Tolerance**: Three-layer fallback mechanism for malformed AI responses
- **Concurrency Protection**: Mutex locks and panic recovery preventing service crashes
- **Frontend Message Delivery Fix**: Global context ensures reliable event transmission
- **Code Cleanup Enhancement**: Isolated quote line removal in generated Go code

---

## 📋 Detailed Changes

### 🚀 New Features

#### Core Architecture (V2)
- **Complete V2 Core**: Unified dispatcher with shared memory, prompt library, and role-based processing
- **MessageBus Integration**: Single-pipe emit with context-aware payload merging
- **State Management**: Unified `RoleState` struct for PM/SE/AP/MC status synchronization
- **Prompt Engineering**: PM Review and AP Approval prompt templates

#### Developer Tools
- **Git Integration (P1)**: Full Git workflow support - status, diff, commit, push, log, branch operations
- **Global Search (P1)**: `search_files` functionality for project-wide file discovery
- **Test Runner**: Integrated test execution with `RunTests/TestConfig/TestReport/parseGoTestOutput`
- **Smart Retry Strategy**: `ClassifyError/ExecuteWithRetry/RetryConfig` for resilient execution
- **AST-level Code Editing**: `ParseGoFile/EditFileWithAST` for precise code modifications
- **Multi-file Context Understanding**: `DependencyAnalyzer/ImpactScope` for cross-file analysis

#### Quality Assurance
- **PM Code Review Phase**: Automated code review after SE execution
- **AP Final Approval (OA)**: Operational approval stage with compliance checking
- **Complete Workflow**: End-to-end quality control pipeline

### 🔧 Bug Fixes

#### Critical Fixes
- **SE Hang Resolution**: Root cause fix for JSON parser fallback + Turn handover + Read timeout
- **Nil Pointer Protection**: Guard against panic dereference in SE ProcessTaskStream
- **Completion Path Fix**: `continueSETask()` missing state setting causing SE hang
- **C-Monitor Intelligence**: Dual-detection mechanism to prevent false resets of completed tasks
- **FALLBACK-FIX Loop Prevention**: Block recursive trigger during PM review phase

#### Frontend & Communication
- **Frontend Message Display**: Fixed message delivery using application global context
- **PM Dual-@ Issue**: Resolved duplicate @ symbol in PM messages
- **Event ACK Repair**: Fixed `pm_started/pm_streaming_done` event acknowledgment
- **Routing Fixes**: Corrected PM/SE message routing and JSON parsing
- **Cross-Monitor Sticky Note**: Fixed position loss across different displays/resolutions

#### Configuration & Stability
- **HTTP Config Persistence**: Fixed port configuration loss after restart
- **Go 1.25 Compatibility**: Updated codebase for Go 1.25 vet and runtime requirements
- **SE Concurrency Control**: Prevented state confusion from concurrent task execution
- **Git Safety**: Prevented working directory Git pollution of main repository
- **API Timeout Protection**: 60-second timeout per call for PM/AP ProcessReview API

#### Code Quality
- **Generated Code Cleanup**: Enhanced trailing garbage removal including isolated quote lines
- **Test Fixes**: Resolved 2 failing tests in chat module

### 📝 Documentation
- **SE Hang Diagnosis**: Comprehensive debugging documents with probe tracking results
- **Git Architecture**: Documented IDE AutoSave mechanism and Git management
- **Bugfix Status Summaries**: Regular status updates on critical issue resolution
- **V2 Design Document**: Complete architecture refactoring specification (`REFACTORING_V2.md` - internal use only)

---

## 📊 Statistics

| Metric | Value |
|--------|-------|
| Total Commits | 38 |
| Time Period | 2026-05-27 ~ 2026-05-31 (4 days) |
| Features Added | 12 major features |
| Bugs Fixed | 18 critical issues |
| Documentation | 8 docs updated/created |
| Code Reduction | ~80% (core modules) |

---

## 🔄 Breaking Changes

None - This release maintains backward compatibility while introducing the V2 architecture as an evolution.

---

## 🚦 Migration Guide

No migration required. The V2 architecture is a drop-in replacement that:
1. Maintains all existing APIs
2. Improves internal structure
3. Enhances reliability and performance
4. Adds new quality assurance phases

---

## 👥 Contributors

- Core Team: Architecture design and implementation
- QA Team: Testing and bug reporting
- Community: Feedback and feature suggestions

---

## 🔮 Next Steps (v0.7.0 Roadmap)

- [ ] Advanced error recovery patterns
- [ ] Performance optimization and profiling
- [ ] Extended tool integration (TODO, DingTalk migration)
- [ ] UI/UX enhancements based on user feedback
- [ ] Additional AI model support and fine-tuning

---

## 📌 Tag Information

```
Tag: v0.6.0
Commit: e421453
Branch: main
Date: 2026-05-31
```

---

*Built with ❤️ by the Argus Team*
*Following the principle: "癞蛤蟆日青蛙——长得丑玩得花" (One actor, multiple roles, brilliant performance)*
