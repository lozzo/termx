# TermX Remote Rebuild

状态：远程能力重建规划入口。

目标：重新建设 TermX 的远程访问体系，复用 `../tgent` 中相对成熟的 web 控制台、hub/TURN、agent 逻辑经验，但避免继续继承 `workspace / tab / pane` 这套 TUI 管理模型。TermX 远程对象模型只保留：

```text
user -> machine -> terminal
```

不管理 TUI layout，不管理 workspace，不管理 tab，不管理 tmux pane。

## 文档

- [总体架构](./architecture.md)
- [连接模式与免费策略](./connection-modes.md)
- [API 草案](./api.md)
- [鉴权与 App 证书配对方案](./auth-and-pairing.md)
- [Mobile App 页面规划](./mobile-app-pages.md)
- [实施批次](./implementation-plan.md)

## 核心结论

1. `termx` 二进制内置远程 agent runtime，随 daemon 一起启动，不再发布独立 agent 程序。
2. `termx` 二进制内置本地 web 静态文件，允许用户通过本机浏览器访问本地控制/调试页。
3. 不登录、不订阅也允许扫码配对并尝试匿名 P2P；匿名路径只用公共 STUN 和轻量 rendezvous signaling。
4. P2P 直连成功时，terminal 和文件管理都可用，因为数据不走 TermX relay。
5. TermX TURN relay 是订阅能力；P2P 失败时引导用户登录并订阅 Relay。
6. 公网 web 控制台负责用户、订阅、机器 claim、token、节点、审计和数据存储。
7. hub/TURN 作为订阅 relay 边缘节点，负责 agent 长连接、managed signaling、ICE/TURN、流量统计和限速。
8. 手机 app 重做产品壳和代码组织，但复用 tgent 的终端、文件、WebRTC、native bridge 行为经验。
9. 网络 transport 必须抽象为 interface。Web runtime 和 native runtime 各自实现，不再把 browser fetch/WebRTC 与 Android native WebRTC 混在同一层业务逻辑里。
10. 远程终端和文件管理直接绑定 `machine_id + terminal_id`，不引入 workspace/tab/pane。
