# R301 旧历史实现清场审计与最小隔离

状态：R301 清场基线；R358 已补充 live/history worker 新边界。

本审计只覆盖 `workflow.md` R301 范围：`termx-core-v2/` 主动修改，`termx-vterm/` 只读。
R302/R303 之前不做大规模删除；本切片只隔离会继续误导后续实现的入口，并留下可编译
guard。

## 结论

- R358 后，真实 PTY 输出路径拆成 live native screen worker 与 history semantic worker。
  live worker 只更新 `live.SurfaceTrack` 当前屏；history worker 把同一 PTY text 喂给
  `vterm.SemanticSource` 生成 `TerminalSemanticTransaction`。
- history 进入 renderer 后只能消费 vterm 的 ordered semantic ops、
  primary scroll-out proof、primary/alt frame payload 和 full-replace boundary。
- `historyANSIParser`、旧 `terminalHistoryPipeline` 和旧 raw fallback 不能恢复为生产 history truth。
- 当前生产 Go 源码未发现 Codex、Claude、htop、vim 等程序名特殊分支。
- 当前未删除 `HistoryTrack` 内的 frame/journal 雏形；这些是定案要求保留的 domain
  概念，后续 R303-R307 需要抽成更清晰的 projector/store/journal contract。

## 隔离动作

- `terminal_semantic_ingest.go` 删除非 shared semantic batch 的自动 raw fallback 分支；
  semantic batch 不再调用 `historyANSIParser` 写入 history。
- 保留 `Ingest` / `IngestBatch` legacy skeleton，但它不在真实 PTY 默认路径上。
- 新增 `r301_history_cleanup_guard_test.go`：
  - 禁止 production Go 源码出现程序名字面量特殊分支。
  - 禁止 semantic batch projector 重新调用 `ingestOutputLocked(batch.Raw)`。
  - 禁止 production terminal watcher 把真实 PTY chunk 送进旧 raw history parser 队列。
  - R358 允许 `terminalHistoryIngestQueue` 作为 history semantic worker backpressure
    边界，但它只能调用 `vterm.SemanticSource` 产出 transaction，不能自行解析 raw history。

## 暂缓到后续切片

- R302：把当前 `vterm.WriteDamage` 整理为正式 `TerminalSemanticTransaction` /
  `TerminalSemanticSource` 接口。
- R303：用 fake semantic transaction 建 projector/domain harness，并把
  `HistoryProjector`、`ScreenAppClassifier`、`HistoryMutation` 边界落地。
- R303 之后：再删除 legacy parser 内的 cursor/alt/fullscreen 补丁语义，避免在接口缺失时
  破坏可编译骨架。
