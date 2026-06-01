# Argus 项目规则

## 编译流程（重要）

修改 Vue 前端代码后，必须执行完整编译流程：

1. 停止运行中的进程
2. 删除旧的 dist
3. 重新编译前端 (npm run build)
4. 重新编译 Go 并打包 (wails build)
5. 运行程序

**Windows 批处理脚本**：`E:\Argus\argus-desktop\build.bat`

**PowerShell 命令**：
```powershell
taskkill /f /im argus-desktop.exe 2>nul
Remove-Item -Recurse -Force frontend/dist -ErrorAction SilentlyContinue
cd frontend; npm run build; cd ..
wails build
./build/bin/argus-desktop.exe
```

**注意**：
- 只运行 `wails build` 不会重新编译前端
- **禁止使用 `go build` 编译**：Wails 应用需要 `wails build` 才能嵌入前端资源（dist目录），直接用 `go build` 会导致程序启动报错 "Wails applications will not build without the correct build tags"
- 必须先 `npm run build` 再 `wails build`

## 核心逻辑修改规则

修改以下核心模块的逻辑必须经过用户确认：

1. **PM（项目经理）核心逻辑**：
   - 状态更新规则（如 `{"action":"update_state","state":N}` 的处理）
   - 任务调度和审核逻辑
   - 消息路由规则

2. **监控模块（C）核心逻辑**：
   - 状态检查规则
   - 消息发送条件
   - 监控启停逻辑

3. **SE（软件工程师）核心逻辑**：
   - 任务执行流程
   - `resp.Completed` 的使用逻辑

**规则**：任何修改上述核心逻辑的代码变更，必须先向用户说明修改原因和方案，获得用户确认后方可实施。

## 文件操作规则（重要）

**禁止使用 PowerShell 操作文件内容**：
- ❌ 禁止：`Set-Content`, `Get-Content | Set-Content`, `(Get-Content) -replace ... | Set-Content`
- ❌ 禁止：任何通过 PowerShell 管道修改文件内容的操作
- **原因**：PowerShell 的 `Set-Content` 会破坏 UTF-8 编码，导致 Go 编译失败（syntax error）
- ✅ 正确做法：使用 **SearchReplace** 工具进行文本替换，该工具保持原始编码不变

**血的教训**：2026-05-13 因使用 `Set-Content` 替换 git.go 中的中文错误消息，导致文件编码损坏，出现 6 处 syntax error，必须 `git checkout` 恢复。

## Git 操作规则

- 用户说"pull"、"拉远程"、"拉最新"时，操作的就是远程仓库，**必须先 `git fetch`**，不要依赖本地缓存的 `origin/main`
- `git reset --hard origin/main` 前如果本地有未push的commit，先询问用户
