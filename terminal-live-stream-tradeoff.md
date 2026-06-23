# 终端实时流慢消费者不可能三角

本文记录 `pty-base-remote` 分支上 PTY byte stream 尝试后的设计结论，避免后续再次把互相冲突的目标塞进同一条 live terminal stream。

## 结论

下面三个目标不能同时成立：

1. 客户端通过 live PTY bytes 获得完整、连续、有序的终端输出，并用它维护自己的 scrollback。
2. 慢客户端、后台 App、慢网络或暂停渲染时，客户端不丢任何 live bytes。
3. 慢客户端绝不反压 core PTY reader、terminal server、transport 或被观察的程序。

如果必须保证完整连续字节流且不允许丢，慢消费者最终一定会把反压传回上游。这是 SSH 这类直连终端的基本模型。反过来，如果必须保证程序不被展示层拖慢，就必须允许客户端丢掉自己的实时窗口，并通过 snapshot/history 重新建立显示状态。

## 常见系统取舍

- SSH 选择完整连续字节流和不丢数据，所以慢网络或慢客户端会反压远端程序。
- tmux/screen 让 server 成为主消费者，由 server 维护 pane 当前屏和历史；客户端消费的是 server 投影，慢客户端可以丢 redraw 或断开，但 server 自己必须能跟上 PTY 和历史维护。
- mosh/current-screen 类模型偏向最新屏状态，慢链路可以丢中间状态，因此不能把 live stream 当作完整 scrollback 或 history truth。

## termx 的边界

termx 后续默认采用下面的边界：

1. core-v2 是 PTY 输出、latest screen 和 logical-line history 的权威维护方。
2. latest screen 用于实时展示、attach/bootstrap、resize/recovery 和当前屏同步。
3. logical-line history 是 copy/search/history/infinite scroll 的权威来源。
4. App/TUI 可以持有本地短 scrollback、xterm/vterm buffer、render cache 或 native bridge backlog，但这些只是显示缓存。
5. App/TUI 不得把本地 live cache 当作 copy/search/history truth。

因此，手机 App 想要滚动历史时，应该走 core-v2 logical-line history API，而不是试图靠 latest screen 或一条永不丢的客户端 PTY live stream 维护完整 scrollback。

## 禁止再次尝试的方向

- 不要把 ordinary live stream 改成“每个客户端都必须完整消费 PTY bytes，且慢客户端不反压程序”。这违反不可能三角。
- 不要用 bounded ring 之后还宣称客户端本地 scrollback 完整。ring 丢旧数据后，客户端本地 live cache 必须被视为不连续。
- 不要让 TUI/App 为了实时显示而在高压输出下解析完整 PTY backlog。显示层可以跳过中间展示状态，history/copy 由 core history 补。
- 不要用 latest screen full frame 推导历史。latest screen 只表达当前屏，不表达完整输出历史。

## 未来设计检查清单

新增 terminal live/display/history 方案前，先回答这几个问题：

1. 这个能力需要完整历史吗？如果需要，数据源必须是 core-v2 logical-line history。
2. 这个能力要求不拖慢程序吗？如果要求，客户端 live 路径必须允许丢窗口、失效或 recovery。
3. 这个能力要求客户端本地完整连续 PTY bytes 吗？如果要求，就必须接受 SSH 式反压，或者引入 server-side durable spool，而不是让普通 live stream 假装同时满足三者。
4. 队列是有界的吗？如果有界，溢出后的 gap/syncLost/cache invalidation 语义必须明确。
5. 中途加入客户端的第一帧来自哪里？默认应来自 core latest screen snapshot；加入前历史来自 core history，不从过去 PTY bytes 补。

一句话：实时显示可以是 latest screen，完整历史必须是 core history；客户端本地 scrollback 只能是缓存，不是权威真值。
