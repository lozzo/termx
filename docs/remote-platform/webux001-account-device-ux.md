# WEBUX001 Web 账号与设备体验

## 1. 范围

本切片只重构普通用户使用 Web Controller 时的账号和设备入口。Controller、手机 activation、daemon enrollment、topology、CommandOutbox、近期认证和 pairing 协议继续由现有服务与 Proto 契约拥有，Web 不建立第二份业务状态。

## 2. 信息架构

一级导航固定为：

1. 概览：设备数量、当前套餐、Relay 余量、系统状态和近期活动。
2. 设备：已注册设备、唯一“添加设备”入口，以及折叠的高级连接详情。
3. 套餐：当前订阅、额度、订单和套餐变更。
4. 账号：用户资料、技术账号身份详情和退出登录。

topology 和 command history 是诊断信息，只能从设备页的“高级连接”折叠区域访问。移动宽度下一级导航使用图标和可访问名称，避免文本互相遮挡。

## 3. 添加设备状态流

“添加设备”先选择设备类型，随后复用已有 Proto API：

```text
选择手机/平板
  -> create mobile activation
  -> 等待 App claim
  -> inspect 并核对设备名称/平台/版本
  -> approve
  -> App complete，关闭向导并刷新设备列表

选择 daemon host
  -> create daemon enrollment
  -> 等待 daemon 提交 DeviceIdentity public key 与 metadata
  -> inspect 并核对名称/主机/平台/版本
  -> approve
  -> daemon proof complete，关闭向导并刷新设备列表
```

二维码和同级手工短码仍指向同一个 flow。Web 只能显示公开 metadata，不接触设备私钥、账号 refresh secret、PairingBundle 或 CapabilityGrant。

## 4. 危险动作

设备撤销、Presence kick、terminal grant revoke 等动作必须先记录具体动作和对象，再打开近期认证对话框。认证成功后只执行用户刚刚确认的动作；页面不维护独立的全局“管理已解锁”真值。服务端仍负责五分钟近期认证窗口和最终授权判断。

## 5. 可用性基线

- 英文和简体中文使用同一页面结构，所有用户文案来自 locale key。
- 360、768、1280、1440 像素视口在 150% CSS 缩放后不得横向溢出或遮挡主要操作。
- 添加设备、高级连接和近期认证必须可以通过键盘抵达和执行。
- 友好名称是列表主标识；device ID、Hub、assignment 和 session identity 位于详情中。

## 6. 验收证据

浏览器交互由 `private/cloud/web-controller/web/e2e/webux001.spec.ts` 覆盖：多视口、150% 缩放、中英文、键盘导航、手机 activation、daemon enrollment 和按动作近期认证。该测试 mock 网络响应，只证明 UI 消费既有 Proto JSON 状态的交互和表现，不冒充 Controller 状态机 E2E。

真实业务状态由现有 Go harness 证明：

- `private/cloud/controller/mobile_activation_test.go` 覆盖创建、claim、等待批准、metadata 核对、批准、完成、单次使用和 session refresh。
- `private/cloud/controller/runtime_test.go` 覆盖 daemon enrollment 创建、等待批准、DeviceIdentity proof、批准、完成、ownership/assignment 持久化和重复 begin 拒绝。

两类证据合并后，才能认定本切片的“真实创建/等待/核对/批准/完成”和用户可用界面均已通过。
