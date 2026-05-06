# TASK_03 — 配置字段与 CLI 命令

**Wave**: 1（无依赖，可与 TASK_01/02/04 同时执行）  
**验证**: `cd termx-remote && go build ./config/... && cd ../termx-cli && go build ./...`

---

## 1. 修改 `termx-remote/config/config.go`

在 `Config` struct 末尾追加三个字段：

```go
Mode     string   // "local" | "online" | "both"，默认 "both"
AllowLAN bool     // local 模式是否允许 LAN IP（false=仅 loopback）
LANIPs   []string // CIDR 或精确 IP 白名单（空=所有私有地址）
```

在 `Normalize()` 函数中追加：

```go
switch strings.ToLower(strings.TrimSpace(c.Mode)) {
case "local", "online", "both":
    c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
case "":
    c.Mode = "both"
default:
    c.Mode = "both" // 非法值静默修正，runtime 层报错
}
```

在文件末尾追加两个辅助函数：

```go
func ModeIncludesLocal(mode string) bool  { return mode == "local" || mode == "both" }
func ModeIncludesOnline(mode string) bool { return mode == "online" || mode == "both" }
```

---

## 2. 修改 `termx-cli/cmd/termx/main.go`

### 2a. 修改 `remote enable` 子命令

找到定义 `--local` 和 `--cloud`（或 `--server`）bool 标志的地方，替换为：

```go
var enableMode  string
var enableToken string
cmd.Flags().StringVar(&enableMode,  "mode",  "both", "connection mode: local, online, or both")
cmd.Flags().StringVar(&enableToken, "token", "",     "access token (required for online/both mode)")
```

命令逻辑（替换原有 `--local`/`--cloud` 分支）：

```go
mode := strings.ToLower(strings.TrimSpace(enableMode))
if mode != "local" && mode != "online" && mode != "both" {
    return fmt.Errorf("--mode must be local, online, or both")
}
if mode != "local" && strings.TrimSpace(enableToken) == "" {
    return fmt.Errorf("--token is required for mode %q", mode)
}
// 设置 cfg.Mode = mode，cfg.AccessToken = enableToken（若非空），cfg.Enabled = true
// 写入配置文件（现有写入逻辑）
```

### 2b. 修改 YAML 配置解析

找到 key-value 读取 remote 配置的 switch/case（或 map lookup）块，添加：

```go
case "mode":
    cfg.Mode = v
case "allow_lan":
    cfg.AllowLAN, _ = strconv.ParseBool(v)
case "lan_ips":
    cfg.LANIPs = splitTrimmed(v) // 逗号或换行分隔
case "token":
    if cfg.AccessToken == "" { cfg.AccessToken = v } // token 是 access_token 的别名
```

如果配置使用结构体反序列化（如 viper / yaml.Unmarshal），则确保 struct tag 包含这些字段：
`mode`、`allow_lan`、`lan_ips`、`token`（alias for `access_token`）。

### 2c. 修改 `buildRemotePairPayload()` 函数

将函数签名从接受单个 `hubURL string` 改为接受 `hubURLs []string`：

```go
func buildRemotePairPayload(
    result *remoteprotocol.PairStartResult,
    status *remoteprotocol.Status,
    hubURLs []string,
) map[string]any {
    payload := map[string]any{
        "type":           "termx_pair_v2",
        "schema_version": 3,
        "machine": map[string]any{
            "id":   result.MachineID,
            "name": firstNonEmpty(result.MachineName, result.MachineID),
        },
        "addresses": map[string]any{
            "local":  []string{},
            "lan":    compactStringSlice(result.LocalPairURL),
            "public": hubURLs,   // 数组，包含所有已注册 hub URL
        },
        "pairing": map[string]any{
            "session_id": result.PairSessionID,
            "secret":     result.PairSecret,
            "expires_at": result.ExpiresAt.Format(time.RFC3339),
        },
    }
    cleanEmptyStrings(payload)
    return payload
}
```

将所有调用 `buildRemotePairPayload(result, status, singleHubURL)` 的地方改为传入
`status.HubURLs`（`[]string`）。如 `status.HubURLs` 为空，则退回到 `[]string{status.HubURL}`。

### 2d. 修改 `remote status` 输出

在 `printRemoteStatus()` 函数中，追加以下输出行（紧跟现有 hub_url 行之后）：

```go
fmt.Fprintf(w, "mode:\t%s\n",     statusValue(status, "mode"))
fmt.Fprintf(w, "allow_lan:\t%v\n", status.AllowLAN)
```
