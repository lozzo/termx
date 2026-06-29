# 工作流：无限历史清场

本文件是当前分支唯一有效的活动驱动文件。当前分支不再继续旧 remote + App 无限历史实现；本轮目标是删除 `feature/infinity-history` 内所有无限历史相关代码和测试，只保留 core/server 的 terminal live、管理、remote transport 基础链路，后续无限历史会重新设计实现。

## 1. 当前目标

- 删除 core-v2 logical-line infinite history 模型、history window/copy/release 协议、客户端 infinite history surface/copy/search/selection 和对应测试。
- 保留 server 路径：terminal create/list/get/restart/remove、PTY live output、input、resize、events、storage、workbench、remote service/transport。
- live terminal 可以继续使用当前 screen/snapshot 作为实时显示投影；不得保留 `history.window`、`history.copy`、`history.release`、history replay、logical-line HistorySource/Surface/Interaction 等无限历史入口。

## 2. 工作范围

允许主动修改：

- `workflow.md`
- `termx-core-v2/`
- `internal/protocol/`
- `termx-proto/`
- `termx-tui-v3/`
- `remote-ui/`
- `termx-app/` 中因 `remote-ui` 导出删除导致的最小联动
- `termx-cli/`、`termx-remote/`、`termx-testkit/` 中因 server/protocol contract 删除导致的最小联动
- `go.work`、`go.work.sum`、必要顶层文档

冻结范围仍不得主动触碰：`termx-core/`、`tuiv2/`、`web-control/`、`termx-hub/`、`bin/`、未列出的实验目录和生成产物目录，除非当前编译确实被删除 contract 影响。

## 3. 硬规则

- 不保留旧无限历史兼容层、fallback、mock source、visual-row history truth、logical-line history window 或 copy/search/selection API。
- 不为通过测试留下空实现的 `history.window`、`history.copy`、`history.release`；协议入口必须消失或明确返回 unsupported。
- server live path 只表达当前实时终端显示，不承担 committed history truth。
- 手工编辑使用 `apply_patch`；不得覆盖未提交的用户改动。
- 每个有效变动必须提交，提交信息使用中文。

## 4. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| R202. SK 删除无限历史实现和测试 | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/`、`termx-tui-v3/`、`remote-ui/`、必要联动 | 删除旧无限历史模型、协议、客户端 surface/copy/search/selection 和测试；保留 server terminal live/management/remote 基础链路 |

## 5. 测试准入

本切片提交前至少运行：

```bash
git diff --check
go test ./termx-core-v2/... ./internal/protocol/... ./termx-proto/...
cd termx-tui-v3 && go test ./...
cd remote-ui && npm run typecheck && npm run test
```

若某个模块因本轮删除需要进一步清理，必须先收敛到可编译再提交。若本机环境缺失导致测试无法运行，最终说明写清原因。
