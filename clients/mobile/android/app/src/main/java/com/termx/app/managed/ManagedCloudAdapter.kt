package com.termx.app.managed

/** Companion 返回给公开 WebRTC primitive 的单个 ICE server。 */
data class ManagedIceServer(val urls: List<String>, val username: String = "", val credential: String = "")

/** Companion endpoint resolution；managedSessionId 只用于 signaling，不是 protocol session 或授权凭据。 */
data class ManagedEndpointResolution(
    val managedSessionId: String,
    val targetDeviceId: String,
    val iceServers: List<ManagedIceServer>,
)

/** 公开 WebRTC primitive 生成的 offer。 */
data class ManagedSignalOffer(val sdp: String)

/** Companion 转发回来的 answer/ICE；它不得包含 grant 或 terminal payload。 */
data class ManagedSignalAnswer(val sdp: String, val candidates: List<String> = emptyList())

/** ManagedCloudLoginFlow 是 Official 模块返回给 Android 壳的短期浏览器设备码投影。 */
data class ManagedCloudLoginFlow(
    val flowId: String,
    val verificationUri: String,
    val userCode: String,
    val expiresAtUnix: Long,
    val pollIntervalMillis: Long,
)

/** ManagedCloudAccount 是可安全投影给 WebView 的账号摘要，不包含 edge token 或浏览器 Session。 */
data class ManagedCloudAccount(val accountId: String, val accountLabel: String, val expiresAtUnix: Long)

/**
 * ManagedCloudAdapter 是移动端私有 cloud module 必须实现的公开边界。
 * 它只能处理 endpoint resolution、signaling 和脱敏质量窗口，禁止接收 grant、设备私钥、DataChannel 或 terminal payload。
 */
interface ManagedCloudAdapter {
    /** beginLogin 创建设备码；Android 壳负责用系统浏览器打开 verificationUri。 */
    suspend fun beginLogin(): ManagedCloudLoginFlow
    /** completeLogin 仅在浏览器批准后持久化 edge session；pending 必须返回 retryable temporary。 */
    suspend fun completeLogin(flowId: String): ManagedCloudAccount
    /** currentAccount 只读取已验证的 Keystore Session 摘要。 */
    suspend fun currentAccount(): ManagedCloudAccount?
    /** logout 删除账号 edge session，不删除 daemon capability grant。 */
    suspend fun logout()
    suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution
    suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer

    /** planManagedRoute 返回显式 smart_route 的短期 direct/single-relay ICE 计划。 */
    suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan

    /** reportPathQuality 转发 GA001A v2 脱敏窗口；实现不得据此自动改路或请求新 lease。 */
    suspend fun reportPathQuality(summary: ManagedPathQualitySummary)
}

/** GrantCredentialStore 只按 grant_ref 从 Android Keystore/Credential Manager 域解析原始 capability。 */
interface GrantCredentialStore {
    suspend fun resolve(grantRef: String): String
}

/**
 * ManagedEndpointAuthorizer 由公开 App 层在 DataChannel 上执行 DeviceIdentity/capability handshake。
 * cloud module 不得实现或绕过该接口；失败时 transport 必须关闭且不能开始 terminal protocol。
 */
interface ManagedEndpointAuthorizer {
    suspend fun authorize(transport: com.termx.app.transport.WebRTCTransport, spec: ManagedEndpointSpec, grant: String)
}

/** CommunityCloudAdapter 明确表示公开 Community build 未包含官方 cloud module。 */
class CommunityCloudAdapter : ManagedCloudAdapter {
    override suspend fun beginLogin(): ManagedCloudLoginFlow = unavailable()
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = unavailable()
    override suspend fun currentAccount(): ManagedCloudAccount? = null
    override suspend fun logout() = Unit

    private fun unavailable(): Nothing {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }

    override suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }

    override suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }

    override suspend fun reportPathQuality(summary: ManagedPathQualitySummary) {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }

    override suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }
}

/** CommunityGrantCredentialStore 不读取旧 session token，也不把 endpoint 配置误当 secret store。 */
class CommunityGrantCredentialStore : GrantCredentialStore {
    override suspend fun resolve(grantRef: String): String {
        throw ManagedEndpointFailure("unauthenticated", "No platform grant credential store is configured")
    }
}

/** CommunityEndpointAuthorizer fail closed，避免未实现端到端授权时开放旧 App 业务通道。 */
class CommunityEndpointAuthorizer : ManagedEndpointAuthorizer {
    override suspend fun authorize(
        transport: com.termx.app.transport.WebRTCTransport,
        spec: ManagedEndpointSpec,
        grant: String,
    ) {
        throw ManagedEndpointFailure("protocol", "Managed endpoint authorizer is unavailable")
    }
}
