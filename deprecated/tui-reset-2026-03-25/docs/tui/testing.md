# termx TUI 测试策略

状态：Draft v3
日期：2026-03-25

## 当前测试原则

当前阶段测试目标不是守住旧 renderer 文本，而是守住：

1. 数据驱动层不坏
2. runtime 主链不坏
3. 新 renderer 落地后能快速重建 E2E

## 当前阶段测试口径

### 保留

- reducer 纯逻辑测试
- domain 纯规则测试
- runtime session / update / input / service 测试
- 最小运行时 smoke

### 暂时不维护

- 旧 renderer 文本快照
- 旧调试 shell 字段断言

## 新 renderer 落地后的 E2E 优先级

### E1 启动可工作

- 启动即进入单 pane shell

### E2 split 可工作

- split 后两个 pane 都能继续输入

### E3 floating 可工作

- floating 可见、可移动、可切换焦点、可调 z-order

### E4 overlay 可工作

- picker / manager / help 能盖在 workbench 上
- 关闭后无残影

### E5 terminal 资源主线

- connect existing
- create terminal
- connect in new tab
- connect in floating pane
- metadata edit
- exited pane restart

## 每轮验证

统一使用仓库内 Go 工具链：

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

## 当前一句话策略

先保住数据层和 runtime，再用新的 renderer 重建真正可用的 E2E。
