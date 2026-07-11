package com.termx.cloud

import android.content.Context
import com.termx.app.managed.ManagedCloudAdapter
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
 * 当前开发构建未注入生产 OAuth/TLS SDK 时稳定返回 login_required，不访问旧 Hub/session-token API。
 */
internal class OfficialCloudGateway(@Suppress("UNUSED_PARAMETER") context: Context) {
    suspend fun resolve(@Suppress("UNUSED_PARAMETER") spec: ManagedEndpointSpec): ManagedEndpointResolution {
        throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
    }

    suspend fun createSignalingSession(
        @Suppress("UNUSED_PARAMETER") spec: ManagedEndpointSpec,
        @Suppress("UNUSED_PARAMETER") resolution: ManagedEndpointResolution,
        @Suppress("UNUSED_PARAMETER") offer: ManagedSignalOffer,
        @Suppress("UNUSED_PARAMETER") policy: ManagedDialPolicy,
    ): ManagedSignalAnswer {
        throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
    }

    suspend fun reportPathQuality(@Suppress("UNUSED_PARAMETER") summary: ManagedPathQualitySummary) {
        throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
    }

    suspend fun planManagedRoute(
        @Suppress("UNUSED_PARAMETER") spec: ManagedEndpointSpec,
        @Suppress("UNUSED_PARAMETER") resolution: ManagedEndpointResolution,
        @Suppress("UNUSED_PARAMETER") policy: ManagedDialPolicy,
    ): ManagedRoutePlan {
        throw ManagedEndpointFailure("login_required", "Official mobile cloud account session is not configured")
    }
}
