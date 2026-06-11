# GitHub 网络故障排查

## 现象
`git push` 报错：
```
fatal: unable to access 'https://github.com/xxx/xxx.git/':
Failed to connect to github.com port 443 after 21000 ms: Could not connect to server
```

## 排查步骤

```powershell
# 1. 检查 DNS 是否正常
Resolve-DnsName github.com -Type A

# 2. 检查常见端口连通性
Test-NetConnection github.com -Port 443  # HTTPS（最常见被墙）
Test-NetConnection github.com -Port 80   # HTTP
Test-NetConnection github.com -Port 22   # SSH
Test-NetConnection ssh.github.com -Port 443  # SSH over HTTPS（备用）
Test-NetConnection 20.205.243.166 -Port 443  # 直连 IP

# 3. 检查代理设置
Get-ChildItem Env: | Where-Object { $_.Name -match "proxy|PROXY" }
git config --global --get-all http.proxy
```

## 解决方案

### 方案一：切 SSH（推荐，本仓库默认已切）
```powershell
# 1. 生成 SSH key（如果还没有）
ssh-keygen -t ed25519 -f "$env:USERPROFILE\.ssh\id_ed25519" -N '""' -q

# 2. 复制公钥
cat ~\.ssh\id_ed25519.pub
# 或：Get-Content ~\.ssh\id_ed25519.pub | Set-Clipboard

# 3. 加到 GitHub
# 浏览器打开 https://github.com/settings/keys → New SSH Key → 粘贴

# 4. 改 remote 为 SSH
git remote set-url origin git@github.com:argustek/Argus.git

# 5. 验证
git push
```

### 方案二：SSH 走 443 端口（SSH 22 也被墙时）
```powershell
# ssh.github.com:443 通常比 direct SSH 更少被限制
git remote set-url origin ssh://git@ssh.github.com:443/argustek/Argus.git
git push
```

### 方案三：设 HTTP 代理
```powershell
git config --global http.proxy http://proxy:port
git config --global https.proxy http://proxy:port
```

## 说明
- SSH URL 只写在本地 `.git/config`，不会提交到仓库，不影响其他电脑
- 其他电脑用 HTTPS clone/pull 不受影响
- 如果回家后 HTTPS 通了，想切回去：`git remote set-url origin https://github.com/argustek/Argus.git`
