package com.termx.cloud

import android.content.Context
import com.termx.app.BuildConfig
import com.termx.app.managed.ManagedCloudAdapter
import com.termx.app.managed.ManagedCloudAccount
import com.termx.app.managed.ManagedCloudClientMetadata
import com.termx.app.managed.ManagedCloudLoginFlow
import com.termx.app.managed.ManagedCloudDevice
import com.termx.app.managed.ManagedCloudModuleFactory
import com.termx.app.managed.ManagedDialPolicy
import com.termx.app.managed.ManagedEndpointFailure
import com.termx.app.managed.ManagedEndpointResolution
import com.termx.app.managed.ManagedEndpointSpec
import com.termx.app.managed.ManagedPathQualitySummary
import com.termx.app.managed.ManagedRoutePlan
import com.termx.app.managed.ManagedSignalAnswer
import com.termx.app.managed.ManagedSignalOffer

/** OfficialManagedCloudFactory 是官方签名 APK 中唯一允许的私有 cloud module 入口。 */
class OfficialManagedCloudFactory : ManagedCloudModuleFactory {
    override fun create(context: Context): ManagedCloudAdapter = OfficialManagedCloudAdapter(OfficialCloudGateway(context.applicationContext))
}

/**
 * OfficialManagedCloudAdapter 只把 endpoint resolution、SDP/ICE signaling 和已校验质量窗口交给私有 gateway。
 * 它不接收 grant、DeviceIdentity private key、DataChannel 或 terminal payload。
 */
internal class OfficialManagedCloudAdapter(private val gateway: OfficialCloudGateway) : ManagedCloudAdapter {
    override suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = gateway.beginLogin(metadata)
    override suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = gateway.claimLogin(userCode, metadata)
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = gateway.completeLogin(flowId)
    override suspend fun currentAccount(): ManagedCloudAccount? = gateway.currentAccount()
    override suspend fun listDevices(): List<ManagedCloudDevice> = gateway.listDevices()
    override suspend fun logout() = gateway.logout()

    override suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution = gateway.resolve(spec)

    override suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer = gateway.createSignalingSession(spec, resolution, offer, policy)

    override suspend fun reportPathQuality(summary: ManagedPathQualitySummary) {
        summary.validate()
        gateway.reportPathQuality(summary)
    }

    override suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan = gateway.planManagedRoute(spec, resolution, policy)
}

/**
 * OfficialCloudGateway 是移动端账号 session、Control Plane 与 Hub SDK 的私有装配点。
 * 只有显式 loopback 或 public HTTP development profile 启用 dev contract；其他 Official 构建继续 fail closed。
 */
internal class OfficialCloudGateway(context: Context) {
    private val development = if (BuildConfig.TERMX_OFFICIAL_DEV_CLOUD_ENABLED) {
        DevCloudMobileGateway(
            BuildConfig.TERMX_OFFICIAL_DEV_CONTROL_URL,
            BuildConfig.TERMX_OFFICIAL_DEV_HUB_URL,
            allowPublicHTTP = BuildConfig.TERMX_OFFICIAL_PUBLIC_HTTP_STAGING_ENABLED,
            sessionStore = AndroidCloudSessionStore(context),
        )
    } else {
        null
    }

    suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution = configured().resolve(spec)

    suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = configured().beginLogin(metadata)
    suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = configured().claimLogin(userCode, metadata)
    suspend fun completeLogin(flowId: String): ManagedCloudAccount = configured().completeLogin(flowId)
    suspend fun currentAccount(): ManagedCloudAccount? = configured().currentAccount()
    suspend fun listDevices(): List<ManagedCloudDevice> = configured().listDevices()
    suspend fun logout() = configured().logout()

    suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer = configured().createSignalingSession(spec, resolution, offer, policy)

    suspend fun reportPathQuality(summary: ManagedPathQualitySummary) = configured().reportPathQuality(summary)

    suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan = configured().planManagedRoute(spec, resolution, policy)

    private fun configured(): DevCloudMobileGateway = development
        ?: throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
}
