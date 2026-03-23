# AGENTS.md

这个文件定义了本仓库内 Codex 和其他代码代理应遵循的本地规则。

## 作用范围

- 默认作用于整个仓库。
- 如果更深层目录里出现新的 `AGENTS.md`，则以更深层文件覆盖对应子目录规则。
- 当前重写主线关注：
  - `tui/`
  - `docs/tui/`
- 旧版 TUI 参考资产位于 `deprecated/tui-legacy/`，默认视为只读参考区。

## 验证要求

- 运行测试时使用仓库内 Go 工具链路径：
  - `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"`
- TUI 重写相关改动的最低验证要求：
  - 跑被修改包的定向 `go test`
  - 跑全量 `go test ./... -count=1`
- 默认工作周期必须是“功能代码 + 相关测试用例”一起完成，不要长期停留在只补测试用例。
- 如果某一轮只能补测试、没有功能代码改动，必须先说明原因，再继续推进。

## 代码风格
- 尽可能的先写interface 然后去实现,用接口解耦系统
- 重点函数,重点代码片段,易混淆片段尽可能写好注释,用中文

## 报告
- 当你完成一轮代码,需要报告人类确认是否继续的时候,请说:"请求确认"
- 还有记得每一轮都要提交代码,使用中文commit信息
- 如果是TUI的更新,请修改 /home/lozzow/workdir/termx/docs/tui/current-status.md 用作状态记录
