# termx-cli

`termx-cli` 是 monorepo 里的命令行产品壳。

职责：

- `termx` 根命令
- daemon / new / ls / attach / kill / web 等命令入口
- 把 `termx-core` 与 `tuiv2` 组装成最终 CLI 行为

## Remote Both Mode Smoke Flow

Use both mode when the same local daemon should expose a LAN local hub and also
register through Web Control for managed access.

1. Login to Web Control:

   ```bash
   termx remote login --server <web-control-url>
   ```

   The command opens the browser/device-code login flow and stores the access
   token in the remote auth store, not in `termx.yaml`.

2. Start the daemon:

   ```bash
   termx daemon &
   ```

   Or run `termx daemon` in a separate terminal; it stays in the foreground.

3. Enable both local and cloud remote paths:

   ```bash
   termx remote enable --mode both --server <web-control-url> --token <access-token>
   ```

   `--token` is required for `online` and `both` modes.

4. Inspect runtime state:

   ```bash
   termx remote status
   termx remote status --json
   ```

   Expected text output includes `local_enabled: true`, `local_web_url`, and
   cloud fields such as `control_url` / `hub_url`. In Web Control, the machine
   should become agent online after hub heartbeat.

5. Run the local smoke checks after `termx remote status` shows `state: online`:

   ```bash
   CONTROL_URL=http://localhost:12306 TERMX_TOKEN=$TERMX_TOKEN ./scripts/smoke-both.sh
   ```

   The script checks the local hub `/api/health`, validates `termx remote status
   --json`, and queries Web Control machines when `TERMX_TOKEN` is set.

6. Open remote-ui for manual terminal verification:

   ```bash
   cd ../remote-ui
   VITE_CONTROL_URL=http://localhost:12306 npm run dev
   ```

   Local path: open `http://localhost:5173/localweb.html`.
   Managed path: open `http://localhost:5173/`, sign in, confirm the machine is
   online, then connect. A usable PTY should accept input in both paths.

开发入口：

```bash
cd termx-cli
go test ./...
go build ./cmd/termx
```
