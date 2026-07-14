package com.termx.app.managed

import android.content.Context

/** Official App 私有 cloud module 必须实现的固定 first-party factory contract。 */
interface ManagedCloudModuleFactory {
    fun create(context: Context): ManagedCloudAdapter
}

/**
 * ManagedCloudAssembly 只查找官方 APK 构建期固定加入的 factory class。
 * Community APK 未包含该 class 时使用 disabled adapter；class 存在但损坏时 fail closed，不回退旧 Hub 或 Community 路径。
 */
object ManagedCloudAssembly {
    private const val OFFICIAL_FACTORY = "com.termx.cloud.OfficialManagedCloudFactory"

    fun create(context: Context): ManagedCloudAdapter {
        val factoryClass = try {
            Class.forName(OFFICIAL_FACTORY)
        } catch (_: ClassNotFoundException) {
            return CommunityCloudAdapter()
        }
        return try {
            val factory = factoryClass.getDeclaredConstructor().newInstance() as? ManagedCloudModuleFactory
                ?: return BrokenOfficialCloudAdapter("Official managed cloud factory has the wrong contract")
            factory.create(context.applicationContext)
        } catch (_: Throwable) {
            BrokenOfficialCloudAdapter("Official managed cloud module could not be loaded")
        }
    }
}

private class BrokenOfficialCloudAdapter(private val detail: String) : ManagedCloudAdapter {
    override suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = unavailable()
    override suspend fun currentAccount(): ManagedCloudAccount? = unavailable()
    override suspend fun listDevices(): List<ManagedCloudDevice> = unavailable()
    override suspend fun logout() = unavailable()

    private fun unavailable(): Nothing {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }

    override suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }

    override suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }

    override suspend fun reportPathQuality(summary: ManagedPathQualitySummary) {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }

    override suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }
}
