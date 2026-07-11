package com.termx.app.managed

/**
 * ManagedRouteSelectionReason 是 Android/TUI 共用的稳定选路诊断。
 * 它不暴露私有候选分数、权重、成本预算或供应商合同。
 */
enum class ManagedRouteSelectionReason(val wireName: String) {
    INITIAL_BEST("initial_best"),
    ONLY_VIABLE("only_viable"),
    LOWER_LOSS("lower_loss"),
    DIRECT_UNSTABLE("direct_unstable"),
    LOWER_LATENCY("lower_latency"),
    LOWER_SCORE("lower_score"),
    COST_GUARD("cost_guard"),
    MINIMUM_HOLD("minimum_hold"),
    COOLDOWN("cooldown"),
    HYSTERESIS_HOLD("hysteresis_hold"),
    INSUFFICIENT_IMPROVEMENT("insufficient_improvement"),
    CURRENT_UNAVAILABLE("current_unavailable"),
    CURRENT_BEST("current_best"),
}

/**
 * ManagedRoutePlan 是官方移动 cloud module 返回给公开 WebRTC primitive 的短期 ICE 计划。
 * 公开层只消费选中路径、原因和 ICE server；它不接收 cost、score、terminal、grant 或 payload。
 */
data class ManagedRoutePlan(
    val planId: String,
    val managedSessionId: String,
    val targetDeviceId: String,
    val selectedPath: ObservedPath,
    val selectionReason: ManagedRouteSelectionReason,
    val validUntilUnix: Long,
    val iceServers: List<ManagedIceServer>,
    val relayOnly: Boolean,
    val relayRegion: String,
) {
    companion object {
        /** WIRE_FIELDS 与公开 Go protobuf 和共享 fixture 的字段顺序一致。 */
        val WIRE_FIELDS = listOf(
            "plan_id", "managed_session_id", "target_device_id", "selected_path", "selection_reason",
            "valid_until_unix", "ice_servers", "relay_only", "relay_region",
        )

        private const val MAX_PLAN_TTL_SECONDS = 600L
        private val REGION_TAG = Regex("[a-z0-9._-]{1,64}")
    }

    /**
     * validate 把计划绑定到当前 endpoint resolution，并拒绝 mesh、过期计划和路径/ICE policy 混淆。
     * 失败计划不能进入 PeerConnection，也不能 fallback 到 resolution 中未选择的 TURN server。
     */
    fun validate(spec: ManagedEndpointSpec, resolution: ManagedEndpointResolution, nowUnix: Long) {
        protocolRequire(planId.isCanonicalRouteID() && managedSessionId == resolution.managedSessionId, "managed route plan identity is invalid")
        protocolRequire(
            targetDeviceId == spec.targetDeviceId && (resolution.targetDeviceId.isBlank() || targetDeviceId == resolution.targetDeviceId),
            "managed route plan target is invalid",
        )
        protocolRequire(
            validUntilUnix > nowUnix && validUntilUnix - nowUnix <= MAX_PLAN_TTL_SECONDS,
            "managed route plan lifetime is invalid",
        )
        protocolRequire(iceServers.all(::isValidIceServer), "managed route plan ICE server is invalid")
        val hasTurn = iceServers.any { server -> server.urls.any(::isTurnURL) }
        when (selectedPath) {
            ObservedPath.DIRECT -> protocolRequire(
                !relayOnly && relayRegion.isEmpty() && !hasTurn,
                "direct route plan contains relay material",
            )
            ObservedPath.SINGLE_RELAY -> protocolRequire(
                relayOnly && hasTurn && relayRegion == relayRegion.trim().lowercase() && REGION_TAG.matches(relayRegion),
                "single-relay route plan is incomplete",
            )
            ObservedPath.RELAY_MESH -> throw ManagedEndpointFailure(
                "protocol",
                "relay mesh is unavailable in the single-relay SmartRoute phase",
            )
        }
    }
}

private fun String.isCanonicalRouteID(): Boolean =
    isNotEmpty() && length <= 128 && this == trim() && none(Char::isWhitespace)

private fun isTurnURL(value: String): Boolean {
    val normalized = value.lowercase()
    return normalized.startsWith("turn:") || normalized.startsWith("turns:")
}

private fun isValidIceServer(server: ManagedIceServer): Boolean {
    if (server.urls.isEmpty()) return false
    for (url in server.urls) {
        if (url.isEmpty() || url != url.trim()) return false
        val normalized = url.lowercase()
        val supported = normalized.startsWith("stun:") || normalized.startsWith("stuns:") ||
            normalized.startsWith("turn:") || normalized.startsWith("turns:")
        if (!supported) return false
        if (isTurnURL(url) && (server.username.isBlank() || server.credential.isBlank())) return false
    }
    return true
}

private fun protocolRequire(condition: Boolean, message: String) {
    if (!condition) throw ManagedEndpointFailure("protocol", message)
}
