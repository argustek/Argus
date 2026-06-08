# Argus GitHub Release 操作指南

> 最后更新：2026-06-08 (v0.7.1)

---

## 目录

1. [获取 Token](#一获取-token)
2. [构建 Exe](#二构建-exe)
3. [创建 Release](#三创建-release)
4. [上传 Exe](#四上传-exe)
5. [一键脚本](#五一键发布脚本)
6. [常见坑位](#六常见坑位总结)
7. [分发说明：光 exe 够不够？](#七分发说明光-exe-够不够)

---

## 一、获取 Token

本项目不需要手动创建 PAT（Personal Access Token），直接从 Git Credential Manager 读取：

```powershell
$tmpFile = [System.IO.Path]::GetTempFileName()
"protocol=https`nhost=github.com`n`n" | git credential fill > $tmpFile 2>$null
$cred = Get-Content $tmpFile
Remove-Item $tmpFile -Force

# 输出如下：
# protocol=https
# host=github.com
# username=argustek
# password=gho_xxxxxxxxxxxx   <-- 这就是 Token

# 提取 token 变量：
$TOKEN = ($cred | Select-String "^password:").ToString().Split("=", 2)[1]
```

**前提**：你之前已经成功 `git push` 过（说明 credential 已配置好）。

如果 `password` 为空，说明 credential 未配置。执行一次 `git push` 会触发登录窗口。

---

## 二、构建 Exe

按项目规则，必须两步构建：

```powershell
# Step 1: 前端构建
cd frontend
npm run build
cd ..

# Step 2: Wails 打包
wails build
```

产物位置：`build/bin/argus-desktop.exe`（约 35MB）

---

## 三、创建 Release

Release 本身通过 **api.github.com** 创建：

### 方式 A：curl + JSON 文件（推荐）

先写一个临时 JSON 文件：

```json
// release.json
{
  "tag_name": "v0.7.1",
  "name": "v0.7.1",
  "body": "## Summary\n\nRelease notes here...",
  "draft": false,
  "prerelease": false
}
```

然后调用 API：

```powershell
curl.exe -s -X POST `
  -H "Authorization: Bearer $TOKEN" `
  -H "Accept: application/vnd.github.v3+json" `
  -H "Content-Type: application/json" `
  --data "@release.json" `
  "https://api.github.com/repos/argustek/Argus/releases"
```

返回的 JSON 中有 `"id"` 字段（如 `336031060`），**记下来**，上传 exe 时要用。

### 方式 B：PowerShell Invoke-RestMethod

```powershell
$body = @{
    tag_name     = "v0.7.1"
    name         = "v0.7.1"
    body         = "Release notes..."
    draft        = $false
    prerelease   = $false
} | ConvertTo-Json -Depth 3

$response = Invoke-RestMethod -Method Post `
  -Uri "https://api.github.com/repos/argustek/Argus/releases" `
  -Headers @{ Authorization = "Bearer $TOKEN"; Accept = "application/vnd.github.v3+json" } `
  -ContentType "application/json; charset=utf-8" `
  -Body ([System.Text.Encoding]::UTF8.GetBytes($body))

$RELEASE_ID = $response.id
Write-Host "Release ID: $RELEASE_ID"
Write-Host "URL: $($response.html_url)"
```

---

## 四、上传 Exe

> **这是最关键的一步！**
>
> 上传文件必须用 **`uploads.github.com`**，不是 `api.github.com`！
>
> 用 api.github.com 上传文件永远返回 **404**（GitHub 的架构设计）

```powershell
curl.exe -s -X POST `
  -H "Authorization: Bearer $TOKEN" `
  -H "Accept: application/vnd.github.v3+json" `
  -H "Content-Type: application/octet-stream" `
  --data-binary "@build/bin/argus-desktop.exe" `
  "https://uploads.github.com/repos/argustek/Argus/releases/{RELEASE_ID}/assets?name=argus-desktop-v0.7.1.exe"
```

**三个必须注意的点：**

| 要点 | 正确 | 错误 |
|------|------|------|
| URL | `uploads.github.com/...` | `api.github.com/...` → **404** |
| Content-Type | `application/octet-stream` | 缺失 → Validation Failed |
| 数据方式 | `--data-binary "@file"` | `-d "@file"` → 文件损坏 |

---

## 五、一键发布脚本

把以下内容保存为 `release.ps1`，在项目根目录执行：

```powershell
param(
    [string]$Version = "v0.7.1"
)

$ErrorActionPreference = "Stop"
Write-Host "=== Argus Release Publisher ===" -ForegroundColor Cyan
Write-Host "Version: $Version"

# 1. 获取 Token
Write-Host "`n[1/5] Acquiring token..." -ForegroundColor Yellow
$tmpFile = [System.IO.Path]::GetTempFileName()
"protocol=https`nhost=github.com`n`n" | git credential fill > $tmpFile 2>$null
$credLine = Get-Content $tmpFile | Select-String "^password:"
Remove-Item $tmpFile -Force
if (-not $credLine) { Write-Host "[ERROR] No token found. Run 'git push' first." -ForegroundColor Red; exit 1 }
$TOKEN = $credLine.ToString().Split("=", 2)[1]
Write-Host "[OK] Token acquired" -ForegroundColor Green

# 2. 检查 exe
Write-Host "`n[2/5] Checking build..." -ForegroundColor Yellow
$exePath = "build/bin/argus-desktop.exe"
if (-not (Test-Path $exePath)) { Write-Host "[ERROR] $exePath not found. Run 'wails build' first." -ForegroundColor Red; exit 1 }
$sizeMb = [math]::Round((Get-Item $exePath).Length / 1MB, 1)
Write-Host "[OK] Exe found (${sizeMb}MB)" -ForegroundColor Green

# 3. 检查 tag
Write-Host "`n[3/5] Checking tag..." -ForegroundColor Yellow
$existingTag = git tag -l "$Version"
if (-not $existingTag) {
    Write-Host "[WARN] Tag $Version not found, creating..." -ForegroundColor DarkYellow
    git tag $Version
    git push origin main --tags
} else {
    Write-Host "[OK] Tag $Version exists" -ForegroundColor Green
}

# 4. 创建 Release
Write-Host "`n[4/5] Creating release..." -ForegroundColor Yellow
$releaseNotesPath = "docs/RELEASE_NOTES_${Version}.md"
$bodyText = if (Test-Path $releaseNotesPath) { Get-Content $releaseNotesPath -Raw } else { "See RELEASE_NOTES for details." }

$releaseBody = @{
    tag_name     = $Version
    name         = $Version
    body         = $bodyText
    draft        = $false
    prerelease   = $false
} | ConvertTo-Json -Depth 3

try {
    $response = Invoke-RestMethod -Method Post `
        -Uri "https://api.github.com/repos/argustek/Argus/releases" `
        -Headers @{ Authorization = "Bearer $TOKEN"; Accept = "application/vnd.github.v3+json" } `
        -ContentType "application/json; charset=utf-8" `
        -Body ([System.Text.Encoding]::UTF8.GetBytes($releaseBody))
    $RELEASE_ID = $response.id
    Write-Host "[OK] Release created: $($response.html_url)" -ForegroundColor Green
} catch {
    # 可能已存在
    if ($_.Exception.Response.StatusCode.value__ -eq 422) {
        Write-Host "[INFO] Release already exists, fetching ID..." -ForegroundColor DarkYellow
        $response = Invoke-RestMethod -Uri "https://api.github.com/repos/argustek/Argus/releases/tags/$Version" `
            -Headers @{ Authorization = "Bearer $TOKEN"; Accept = "application/vnd.github.v3+json" }
        $RELEASE_ID = $response.id
        Write-Host "[OK] Using existing release (ID: $RELEASE_ID)" -ForegroundColor Green
    } else { throw }
}

# 5. 上传 Exe
Write-Host "`n[5/5] Uploading exe (~${sizeMb}MB, please wait)..." -ForegroundColor Yellow
$assetName = "argus-desktop-${Version}.exe"
$uploadResult = curl.exe -s -X POST `
    -H "Authorization: Bearer $TOKEN" `
    -H "Accept: application/vnd.github.v3+json" `
    -H "Content-Type: application/octet-stream" `
    --data-binary "@$exePath" `
    "https://uploads.github.com/repos/argustek/Argus/releases/$RELEASE_ID/assets?name=$assetName"

$uploadObj = $uploadResult | ConvertFrom-Json
if ($uploadObj.state -eq "uploaded") {
    Write-Host "[DONE] Upload complete!" -ForegroundColor Green
    Write-Host "Download: $($uploadObj.browser_download_url)" -ForegroundColor Cyan
} else {
    Write-Host "[ERROR] Upload failed: $uploadResult" -ForegroundColor Red
}
```

使用方式：

```powershell
# 默认版本
.\release.ps1

# 指定版本
.\release.ps1 -Version "v0.8.0"
```

---

## 六、常见坑位总结

| # | 坑 | 现象 | 解决方法 |
|---|-----|------|----------|
| 1 | 用 `api.github.com` 上传文件 | HTTP 404 Not Found | 改用 `uploads.github.com` |
| 2 | 缺少 Content-Type 头 | Validation Failed: content_type can't be application/x-www-form-urlencoded | 加 `-H "Content-Type: application/octet-stream"` |
| 3 | 用 `-d` 替代 `--data-binary` | 下载的 exe 无法运行（损坏） | 必须用 `--data-binary "@file"` |
| 4 | 用 Invoke-RestMethod 上传大文件 | 内存占用爆炸 / 超时 | 改用 `curl.exe` |
| 5 | PowerShell here-string 在单行命令中 | ParserError: Unexpected token | 写 JSON 文件 + `--data "@file"` |
| 6 | tag 未 push 就创建 Release | API 报错找不到 ref | 先 `git push origin main --tags` |
| 7 | git credential 未配置 | password 为空 | 先 `git push` 触发登录 |
| 8 | 同名 asset 重复上传 | 422 Conflict | 先删除旧 asset 或改名 |

**核心口诀：api 创建，uploads 上传。**

---

## 七、分发说明：光 exe 够不够？

### 当前状态：单 exe 可运行（但有条件）

Wails v2 构建的 `argus-desktop.exe` 是一个独立可执行文件，**双击即可运行**，不需要安装。

但有以下前置条件：

| 条件 | 说明 |
|------|------|
| **WebView2 Runtime** | Windows 10 (19044+) 和 Windows 11 通常已内置。老版本 Win10 需要单独安装 |
| **操作系统** | Windows x64 (Windows 10 19044+ / Windows 11) |
| **杀毒软件** | 首次运行可能被拦截（无签名），需手动信任 |

### 用户首次运行可能遇到的问题

#### 问题 1：缺少 WebView2

**现象**：启动后闪退或报错 "WebView2 not found"

**解决**：用户需安装 Microsoft Edge WebView2 Runtime：
- 下载地址：https://developer.microsoft.com/en-us/microsoft-edge/webview2/
- 永久链接：https://go.microsoft.com/fwlink/p/?LinkId=2124703

#### 问题 2：Windows SmartScreen 拦截

**现象**：下载后提示 "Windows 保护了你的电脑"

**解决**：点击"更多信息" → "仍要运行"

#### 问题 3：无代码签名

**现象**：发布者显示"未知"

**影响**：不影响功能，但会降低用户信任度

**后续改进方向**（可选）：

| 方案 | 说明 | 复杂度 |
|------|------|--------|
| **代码签名证书** | 购买 EV 代码签名证书对 exe 签名 | 中（需购买证书 ~$200-400/年） |
| **NSIS 安装包** | 打包成 setup.exe，自动检测/安装 WebView2 | 低（加个 NSIS 脚本就行） |
| **ZIP + 说明文件** | 把 exe 打包成 zip，附带 README 说明运行条件 | 最低（推荐先做这个） |

### 推荐的最小化分发方案（立即可做）

在 Release 页面的描述中加上运行要求：

```markdown
## System Requirements
- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage
1. Download `argus-desktop-vX.Y.Z.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
```

### 未来：制作安装包（TODO）

如果要做成专业的安装程序，可以用 NSIS 或 Inno Setup：

```nsis
; 示例 NSIS 脚本结构（未来实现）
!include MUI2.nsh
Name "Argus"
OutFile "argus-desktop-setup-v0.7.1.exe"
InstallDir "$PROGRAMFILES64\Argus"

; 检测并安装 WebView2
Section "WebView2 Runtime"
    ; 下载并静默安装 WebView2 bootstrapper
SectionEnd

; 复制 exe
Section "Argus"
    SetOutPath $INSTDIR
    File "build\bin\argus-desktop.exe"
    CreateShortcut "$DESKTOP\Argus.lnk" "$INSTDIR\argus-desktop.exe"
SectionEnd
```

这可以作为一个后续任务来实现。
