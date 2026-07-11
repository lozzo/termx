package com.termx.app.managed

/**
 * ManagedEndpointSpec 是 Android App 与 TUI 共用语义的 endpoint 配置投影。
 * endpointId 是客户端稳定主键；targetDeviceId 只用于云路由；deviceFingerprint 才是 daemon 信任锚点；grantRef 只引用平台凭据存储。
 */
data class ManagedEndpointSpec(
    val endpointId: String,
    val targetDeviceId: String,
    val deviceFingerprint: String,
    val grantRef: String,
    val relayMode: RelayMode,
)

/** Managed endpoint 的统一连接阶段；它不代表 daemon terminal lifecycle。 */
enum class ManagedEndpointPhase(val wireName: String) {
    IDLE("idle"),
    RESOLVING("resolving"),
    SIGNALING("signaling"),
    CONNECTING("connecting"),
    AUTHORIZING("authorizing"),
    CONNECTED("connected"),
    FAILED("failed"),
}

/** WebRTC 的实际路径；三种值始终属于同一个 endpoint transport。 */
enum class ObservedPath(val wireName: String) {
    DIRECT("direct"),
    SINGLE_RELAY("single_relay"),
    RELAY_MESH("relay_mesh"),
}

/** connection registry 的 relay_mode 稳定值。 */
enum class RelayMode(val wireName: String) {
    AUTO("auto"),
    DIRECT("direct"),
    RELAY_ONLY("relay_only"),
    SMART_ROUTE("smart_route");

    companion object {
        /** fromWire 严格解析 registry 值；未知值不能回退为 auto。 */
        fun fromWire(value: String): RelayMode = entries.firstOrNull { it.wireName == value.trim() }
            ?: throw ManagedEndpointFailure("protocol", "unknown managed WebRTC relay mode")
    }
}

/** Companion route preference 与公开 WebRTC ICE policy 的统一映射结果。 */
data class ManagedDialPolicy(val routePreference: String, val relayOnly: Boolean)

/** ManagedEndpointFailure 是 App 可以稳定投影的 endpoint 局部错误。 */
class ManagedEndpointFailure(val code: String, message: String) : Exception(message)

/** ManagedEndpointContract 是 Android 对共享 JSON fixture 的纯领域实现。 */
object ManagedEndpointContract {
    /** validate 在访问 Companion 或凭据存储前校验 endpoint identity 的必要字段。 */
    fun validate(spec: ManagedEndpointSpec) {
        if (spec.endpointId.isBlank()) throw ManagedEndpointFailure("protocol", "endpoint_id is required")
        if (spec.targetDeviceId.isBlank()) throw ManagedEndpointFailure("protocol", "target_device_id is required")
        if (spec.deviceFingerprint.isBlank()) throw ManagedEndpointFailure("protocol", "device_fingerprint is required")
        if (spec.grantRef.isBlank() || spec.grantRef.any(Char::isWhitespace)) {
            throw ManagedEndpointFailure("unauthenticated", "grant_ref is required")
        }
    }

    /** dialPolicy 与 Go cloudcompanion.DialPolicyForRelayMode 保持同一 fixture 语义。 */
    fun dialPolicy(mode: RelayMode): ManagedDialPolicy = when (mode) {
        RelayMode.AUTO -> ManagedDialPolicy("standard_relay", false)
        RelayMode.DIRECT -> ManagedDialPolicy("direct_only", false)
        RelayMode.RELAY_ONLY -> ManagedDialPolicy("standard_relay", true)
        RelayMode.SMART_ROUTE -> ManagedDialPolicy("smart_route", false)
    }
}
