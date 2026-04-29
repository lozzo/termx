# tuiv2

`tuiv2` 是 monorepo 里的 TUI 库模块。

它提供：

- input / modal / render
- runtime / attach / local interaction orchestration
- workbench projection
- TUI 专属配置与主题系统

它依赖 `termx-core` 的 public interface，但不要求 `termx-core` 反向依赖自己。

## 测试

```bash
go test ./...
```

## 文档

见 [docs/README.md](docs/README.md)。
