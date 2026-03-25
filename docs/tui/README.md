# termx TUI 文档

状态：Draft v3
日期：2026-03-25

这次文档重置只保留少量核心文件，目标是：

- 减少重复口径
- 直接服务编码
- 明确“保留数据层，重写 renderer”

## 文档集合

1. [`current-status.md`](current-status.md)
- 当前阶段判断
- 当前禁区
- 下一阶段直接做什么

2. [`product-spec.md`](product-spec.md)
- 产品概念
- 用户主路径
- 核心对象和状态

3. [`architecture.md`](architecture.md)
- 当前仓库已保留的层
- 新 renderer 重写边界
- 旧版 legacy renderer 可回收骨架

4. [`wireframes.md`](wireframes.md)
- 目标界面骨架
- tiled / floating / overlay 的基本布局

5. [`roadmap.md`](roadmap.md)
- 分阶段工作块
- 当前阶段收口顺序

6. [`testing.md`](testing.md)
- E2E 优先测试口径
- 当前重写阶段的测试策略

## 推荐阅读顺序

1. `current-status.md`
2. `product-spec.md`
3. `architecture.md`
4. `wireframes.md`
5. `roadmap.md`
6. `testing.md`

## 参考区

- 旧版参考资产：`deprecated/tui-legacy/`
- 当前文档默认以“保数据驱动层、重写 renderer”为统一口径
