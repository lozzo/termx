package com.termx.app.managed

import termx.cloud.v1.CloudCompanion

/** ManagedEndpointFailure 是 Android platform adapter 映射到稳定 ApiError 的失败边界。 */
class ManagedEndpointFailure(val code: String, message: String) : Exception(message)

/** ManagedCloudLoginFlow 是 Official 模块返回给 Android 壳的短期浏览器设备码投影。 */
data class ManagedCloudLoginFlow(
    val flowId: String,
    val verificationUri: String,
    val userCode: String,
    val expiresAtUnix: Long,
    val pollIntervalMillis: Long,
)

/** ManagedCloudClientMetadata 是 Web 批准页允许展示的非秘密 Official App 设备信息。 */
data class ManagedCloudClientMetadata(
    val displayName: String,
    val platform: String,
    val termxVersion: String,
)

/** ManagedCloudAccount 是可安全投影给 WebView 的账号摘要，不包含 edge token 或浏览器 Session。 */
data class ManagedCloudAccount(val accountId: String, val accountLabel: String, val expiresAtUnix: Long)

/** ManagedCloudDevice 是 Hub 同账号内存目录的非秘密投影；它不代表已持有 daemon capability。 */
data class ManagedCloudDevice(
    val deviceId: String,
    val deviceFingerprint: String,
    val displayName: String,
    val platform: String,
    val kind: String,
    val online: Boolean,
    val revoked: Boolean,
)

/**
 * ManagedCloudAdapter 是移动端私有 cloud module 必须实现的公开边界。
 * 它只处理账号控制面与生成的 cloudpb 请求，禁止接收 grant、设备私钥、DataChannel 或 terminal payload。
 */
interface ManagedCloudProtoAdapter {
    suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = protoUnavailable()
    suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = protoUnavailable()
    suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = protoUnavailable()
    suspend fun planRouteProto(request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan = protoUnavailable()
    suspend fun reportQualityProto(request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse = protoUnavailable()
    suspend fun reportOutcomeProto(request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse = protoUnavailable()

    private fun protoUnavailable(): Nothing =
        throw ManagedEndpointFailure("companion_missing", "Managed Cloud Proto adapter is not configured")
}

interface ManagedCloudAdapter : ManagedCloudProtoAdapter {
    /** beginLogin 创建由 Web 批准的短码 Flow；调用方不得自动打开浏览器。 */
    suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow
    /** claimLogin 用 Web 二维码中的短期 locator 认领 Flow，高熵 flow ID 只返回原生层。 */
    suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow
    /** completeLogin 仅在浏览器批准后持久化 edge session；pending 必须返回 retryable temporary。 */
    suspend fun completeLogin(flowId: String): ManagedCloudAccount
    /** currentAccount 只读取已验证的 Keystore Session 摘要。 */
    suspend fun currentAccount(): ManagedCloudAccount?
    /** listDevices 只做账号节点发现；调用方仍须从 native grant store 判断是否已配对。 */
    suspend fun listDevices(): List<ManagedCloudDevice>
    /** logout 删除账号 edge session，不删除 daemon capability grant。 */
    suspend fun logout()
}

/** CommunityCloudAdapter 明确表示公开 Community build 未包含官方 cloud module。 */
class CommunityCloudAdapter : ManagedCloudAdapter {
    override suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = unavailable()
    override suspend fun currentAccount(): ManagedCloudAccount? = null
    override suspend fun listDevices(): List<ManagedCloudDevice> = unavailable()
    override suspend fun logout() = Unit
    override suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = unavailable()
    override suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = unavailable()
    override suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = unavailable()
    override suspend fun planRouteProto(request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan = unavailable()
    override suspend fun reportQualityProto(request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse = unavailable()
    override suspend fun reportOutcomeProto(request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse = unavailable()

    private fun unavailable(): Nothing {
        throw ManagedEndpointFailure("companion_missing", "Official managed cloud module is not installed")
    }

}
