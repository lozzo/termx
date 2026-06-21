# termx-cli

`termx-cli` 是 monorepo 里的命令行产品壳。

职责：

- `termx` 根命令
- daemon / new / ls / attach / kill / web 等命令入口
- 把 `termx-core-v2` 与 `termx-tui-v3` 组装成最终 CLI 行为

## Remote Both Mode Smoke Flow

Use both mode when the same local daemon should expose a LAN local hub and also
register through Web Control for Hub access.

1. Start the daemon:

   ```bash
   termx daemon &
   ```

   Or run `termx daemon` in a separate terminal; it stays in the foreground.

2. Enable both local and Hub remote paths:

   ```bash
   termx remote enable --mode both
   ```

   The Web Control endpoint is fixed by default. Self-hosted/test builds can
   set it through `remote.control_url` or `TERMX_REMOTE_CONTROL_URL`.

   Without `--token`, the command opens the browser login flow, stores the
   returned access token in the remote auth store, and keeps the raw token out
   of `termx.yaml`. Use `--token <access-token>` only for automation, or
   `--browser` to force a fresh browser login over any saved token.

3. Inspect runtime state:

   ```bash
   termx remote status
   termx remote status --json
   ```

   Expected text output includes `local_enabled: true`, `local_web_url`, and
   Hub fields such as `control_url` / `hub_url`. In Web Control, the machine
   should become agent online after hub heartbeat.

4. Run the local smoke checks after `termx remote status` shows `state: online`:

   ```bash
   CONTROL_URL=http://localhost:12306 TERMX_TOKEN=$TERMX_TOKEN ./scripts/smoke-both.sh
   ```

   The script checks the local hub `/api/health`, validates `termx remote status
   --json`, and queries Web Control machines when `TERMX_TOKEN` is set.

5. Open remote-ui for manual terminal verification:

   ```bash
   cd ../remote-ui
   VITE_CONTROL_URL=http://localhost:12306 npm run dev
   ```

   Local path: open `http://localhost:5173/localweb.html`.
   Hub path: open `http://localhost:5173/`, sign in, confirm the machine is
   online, then connect. A usable PTY should accept input in both paths.

开发入口：

```bash
cd termx-cli
go test ./...
go build ./cmd/termx
```
