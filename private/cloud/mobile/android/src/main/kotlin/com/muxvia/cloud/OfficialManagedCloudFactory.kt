package com.muxvia.cloud

import android.content.Context
import com.muxvia.app.BuildConfig
import com.muxvia.app.managed.ManagedCloudAdapter
import com.muxvia.app.managed.ManagedCloudAccount
import com.muxvia.app.managed.ManagedCloudClientMetadata
import com.muxvia.app.managed.ManagedCloudLoginFlow
import com.muxvia.app.managed.ManagedCloudDevice
import com.muxvia.app.managed.ManagedCloudModuleFactory
import com.muxvia.app.managed.ManagedEndpointFailure
import muxvia.cloud.v1.CloudCompanion
import muxvia.client.binding.v1.ClientBinding

/** OfficialManagedCloudFactory 是官方签名 APK 中唯一允许的私有 cloud module 入口。 */
class OfficialManagedCloudFactory : ManagedCloudModuleFactory {
    override fun create(context: Context): ManagedCloudAdapter = OfficialManagedCloudAdapter(OfficialCloudGateway(context.applicationContext))
}

/**
 * OfficialManagedCloudAdapter 只把 endpoint resolution、SDP/ICE signaling 和已校验质量窗口交给私有 gateway。
 * 它不接收 grant、DeviceIdentity private key、DataChannel 或 terminal payload。
 */
internal class OfficialManagedCloudAdapter(private val gateway: OfficialCloudGateway) : ManagedCloudAdapter {
    override suspend fun routeEligibilityProto(request: ClientBinding.CloudRouteEligibilityRequest): ClientBinding.CloudRouteEligibility = gateway.routeEligibilityProto(request)
    override suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = gateway.beginLogin(metadata)
    override suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = gateway.claimLogin(userCode, metadata)
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = gateway.completeLogin(flowId)
    override suspend fun currentAccount(): ManagedCloudAccount? = gateway.currentAccount()
    override suspend fun listDevices(): List<ManagedCloudDevice> = gateway.listDevices()
    override suspend fun logout() = gateway.logout()
    override suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = gateway.resolveProto(request)
    override suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = gateway.createSignalingProto(request)
    override suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = gateway.acquireRelayProto(request)
    override suspend fun planRouteProto(request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan = gateway.planRouteProto(request)
    override suspend fun reportQualityProto(request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse = gateway.reportQualityProto(request)
    override suspend fun reportOutcomeProto(request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse = gateway.reportOutcomeProto(request)

}

/**
 * OfficialCloudGateway 是移动端账号 session、Control Plane 与 Hub SDK 的私有装配点。
 * 只有显式 loopback 或 public HTTP development profile 启用 dev contract；其他 Official 构建继续 fail closed。
 */
internal class OfficialCloudGateway(context: Context) {
    private val development = if (BuildConfig.MUXVIA_DEV_CLOUD_ENABLED) {
        DevCloudMobileGateway(
            BuildConfig.MUXVIA_DEV_CONTROL_URL,
            BuildConfig.MUXVIA_DEV_HUB_URL,
            allowPublicHTTP = BuildConfig.MUXVIA_PUBLIC_HTTP_STAGING_ENABLED,
            sessionStore = AndroidCloudSessionStore(context),
        )
    } else {
        null
    }

    suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = configured().resolveProto(request)
    suspend fun routeEligibilityProto(request: ClientBinding.CloudRouteEligibilityRequest): ClientBinding.CloudRouteEligibility =
        if (development == null) {
            ClientBinding.CloudRouteEligibility.getDefaultInstance()
        } else {
            development.routeEligibilityProto(request)
        }
    suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = configured().createSignalingProto(request)
    suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = configured().acquireRelayProto(request)
    suspend fun planRouteProto(request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan = configured().planRouteProto(request)
    suspend fun reportQualityProto(request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse = configured().reportQualityProto(request)
    suspend fun reportOutcomeProto(request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse = configured().reportOutcomeProto(request)

    suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = configured().beginLogin(metadata)
    suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = configured().claimLogin(userCode, metadata)
    suspend fun completeLogin(flowId: String): ManagedCloudAccount = configured().completeLogin(flowId)
    suspend fun currentAccount(): ManagedCloudAccount? = configured().currentAccount()
    suspend fun listDevices(): List<ManagedCloudDevice> = configured().listDevices()
    suspend fun logout() = configured().logout()

    private fun configured(): DevCloudMobileGateway = development
        ?: throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
}
