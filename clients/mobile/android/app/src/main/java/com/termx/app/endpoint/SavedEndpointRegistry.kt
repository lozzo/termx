package com.termx.app.endpoint

/** EndpointContractException 是 Android native registry 与 assembler 的稳定失败。 */
class EndpointContractException(
    /** code 与 Go/TypeScript 共享，UI 只能按该字段选择恢复动作。 */
    val code: String,
    message: String,
) : IllegalArgumentException(message)

/** EndpointRouteKind 表示到达同一 daemon Endpoint 的持久 route 类型，不是一次运行时 transport。 */
enum class EndpointRouteKind(val wireName: String) {
    LOCAL_UNIX("local-unix"),
    SSH_STDIO("ssh-stdio"),
    DIRECT_TLS("direct-tls"),
    MANAGED_WEBRTC("managed-webrtc");

    companion object {
        internal fun parse(value: String): EndpointRouteKind = entries.firstOrNull { it.wireName == value }
            ?: fail("config_invalid", "unknown route kind")
    }
}

/** EndpointConnectMode 只决定客户端何时发起连接，不拥有 daemon terminal lifecycle。 */
enum class EndpointConnectMode(val wireName: String) {
    AUTO("auto"),
    ON_DEMAND("on_demand"),
    MANUAL("manual");

    companion object {
        internal fun parse(value: String): EndpointConnectMode = entries.firstOrNull { it.wireName == value }
            ?: fail("config_invalid", "unknown connect mode")
    }
}

/** EndpointRelayMode 只描述 managed WebRTC 内部 direct/single Relay 约束。 */
enum class EndpointRelayMode(val wireName: String) {
    AUTO("auto"),
    DIRECT("direct"),
    RELAY_ONLY("relay_only"),
    SMART_ROUTE("smart_route");

    companion object {
        internal fun parse(value: String): EndpointRelayMode = entries.firstOrNull { it.wireName == value }
            ?: fail("config_invalid", "unknown relay mode")
    }
}

/** EndpointSource 记录配置来源；只有已确认 share/user 输入可以覆盖本地选择策略。 */
enum class EndpointSource(val wireName: String, internal val rank: Int) {
    LAN("lan", 0),
    CLOUD("cloud", 10),
    BOOTSTRAP("bootstrap", 20),
    LOCAL("local", 25),
    MANUAL("manual", 30),
    SHARE("share", 40),
    USER("user", 50);

    companion object {
        internal fun parse(value: String): EndpointSource = entries.firstOrNull { it.wireName == value }
            ?: fail("config_invalid", "unknown endpoint source")
    }
}

/** EndpointCredentialKind 只描述目标 Android secure store 需要解析的凭据类别，不携带凭据 body。 */
enum class EndpointCredentialKind(val wireName: String) {
    SSH_AGENT("ssh-agent"),
    SSH_PRIVATE_KEY("ssh-private-key"),
    SSH_PASSWORD("ssh-password"),
    CAPABILITY_GRANT("capability-grant"),
    CLOUD_PROFILE("cloud-profile");

    companion object {
        internal fun parse(value: String): EndpointCredentialKind = entries.firstOrNull { it.wireName == value }
            ?: fail("config_invalid", "unknown credential descriptor kind")
    }
}

/** EndpointDaemonIdentity 是 DeviceID 与长期 public-key fingerprint 组成的 daemon 安全 pin。 */
data class EndpointDaemonIdentity(
    val deviceId: String = "",
    val deviceFingerprint: String = "",
) {
    internal fun isEmpty(): Boolean = deviceId.isEmpty() && deviceFingerprint.isEmpty()

    internal fun validate(required: Boolean) {
        if (isEmpty() && !required) return
        val invalidIdentityCharacter: (Char) -> Boolean = { it.isWhitespace() || Character.isISOControl(it) }
        if (deviceId.isBlank() || deviceFingerprint.isBlank() || deviceId.any(invalidIdentityCharacter) || deviceFingerprint.any(invalidIdentityCharacter)) {
            fail("config_invalid", "daemon identity requires a valid device_id and device_fingerprint")
        }
    }
}

/** EndpointSelectionPolicy 保存客户端本地 grouped-hedge 延迟；route priority 仍属于各 route。 */
data class EndpointSelectionPolicy(
    val hedgeDelayMillis: Long = 0,
    val hedgeDelayConfigured: Boolean = false,
)

/** SavedAccessRoute 是 Android native 持久化的一条 route 配置；credentialRef 只引用本机 secure store。 */
data class SavedAccessRoute(
    val routeId: String,
    val kind: EndpointRouteKind,
    val enabled: Boolean = true,
    val manualOnly: Boolean = false,
    val priority: Int? = null,
    val credentialRef: String = "",
    val source: EndpointSource = EndpointSource.MANUAL,
    val policySource: EndpointSource = source,
    val socket: String = "",
    val host: String = "",
    val port: Int = 0,
    val user: String = "",
    val proxyJump: String = "",
    val remoteSocket: String = "",
    val hostKeyFingerprints: List<String> = emptyList(),
    val addresses: List<String> = emptyList(),
    val serverName: String = "",
    val targetDeviceId: String = "",
    val accountProfile: String = "",
    val relayMode: EndpointRelayMode? = null,
)

/** SavedEndpoint 是 Android 本地一个 daemon 的稳定记录；Routes 是到达方式，不能复制 terminal/history truth。 */
data class SavedEndpoint(
    val endpointId: String,
    val label: String,
    val labelSource: EndpointSource = EndpointSource.MANUAL,
    val daemonIdentity: EndpointDaemonIdentity = EndpointDaemonIdentity(),
    val connectMode: EndpointConnectMode = EndpointConnectMode.ON_DEMAND,
    val enabled: Boolean = true,
    val selectionPolicy: EndpointSelectionPolicy = EndpointSelectionPolicy(),
    val routes: Map<String, SavedAccessRoute>,
)

/** SavedEndpointRegistry 是 Official App 冷启动首先读取的 Endpoint 期望状态真值。 */
data class SavedEndpointRegistry(
    val version: Int = ENDPOINT_REGISTRY_VERSION,
    val defaultEndpointId: String = "",
    val endpoints: Map<String, SavedEndpoint> = emptyMap(),
)

/**
 * AndroidCredentialDescriptor 是 assembler 输出给目标端 secure-store 事务的脱敏说明。
 * descriptorId 不是源平台 credential ref；secret、Cloud token 和源 CapabilityGrant 不得进入该结构。
 */
data class AndroidCredentialDescriptor(
    val descriptorId: String,
    val kind: EndpointCredentialKind,
    val exportable: Boolean = false,
)

/** AndroidEndpointCandidate 是 Cloud/bootstrap/manual/share 经各自验证后交给 native assembler 的纯输入。 */
data class AndroidEndpointCandidate(
    val source: EndpointSource,
    val identity: EndpointDaemonIdentity,
    val suggestedLabel: String = "",
    val routes: List<SavedAccessRoute> = emptyList(),
    val connectMode: EndpointConnectMode? = null,
    val selectionPolicy: EndpointSelectionPolicy? = null,
    val applyClientPolicy: Boolean = false,
    val credentialDescriptors: List<AndroidCredentialDescriptor> = emptyList(),
)

/**
 * ConfirmedEndpointIdentityBinding 是用户确认把 identity 为空的本地/SSH Endpoint 绑定到已验证 daemon 的本地事务输入。
 * identity 必须同时出现在本次 candidates 中；Cloud/bootstrap/share payload 不能自行指定本地 endpointId。
 */
data class ConfirmedEndpointIdentityBinding(
    val endpointId: String,
    val identity: EndpointDaemonIdentity,
)

/** AndroidEndpointAssemblerResult 返回新的 registry snapshot；调用方应在 secure credential 提交成功后再发布。 */
data class AndroidEndpointAssemblerResult(
    val registry: SavedEndpointRegistry,
    val resolvedEndpointIds: List<String>,
    val credentialDescriptors: List<AndroidCredentialDescriptor>,
)

/** Android registry 当前唯一受支持版本。 */
const val ENDPOINT_REGISTRY_VERSION = 2

/** Android 普通 registry 与共享 fixture 的最大输入大小。 */
const val ENDPOINT_REGISTRY_MAX_BYTES = 1 shl 20

internal fun validateCredentialDescriptor(descriptor: AndroidCredentialDescriptor) {
	if (!Regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$").matches(descriptor.descriptorId)) {
        fail("config_invalid", "credential descriptor requires a single-line id")
    }
    if (descriptor.exportable && descriptor.kind !in setOf(EndpointCredentialKind.SSH_PRIVATE_KEY, EndpointCredentialKind.SSH_PASSWORD)) {
        fail("config_invalid", "credential descriptor kind cannot be exported")
    }
}

internal fun fail(code: String, message: String): Nothing = throw EndpointContractException(code, message)
