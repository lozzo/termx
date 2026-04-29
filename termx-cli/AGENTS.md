# termx-cli Agent Notes

当前项目根目录：`termx-cli/`

## Boundary

- `termx-cli` 是产品壳，不是 core。
- 可以依赖 `termx-core` 和 `tuiv2` 的 public package。
- 不要把新的 shell-neutral 能力继续塞回 CLI。
