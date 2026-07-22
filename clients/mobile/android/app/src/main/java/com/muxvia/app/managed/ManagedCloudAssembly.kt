package com.muxvia.app.managed

import android.content.Context
import android.util.Log
import muxvia.cloud.v1.CloudCompanion
import muxvia.client.binding.v1.ClientBinding

/** 单一 Muxvia App 私有 cloud module 必须实现的固定 first-party factory contract。 */
interface ManagedCloudModuleFactory {
    fun create(context: Context): ManagedCloudAdapter
}

/**
 * ManagedCloudAssembly 只查找标准 APK 构建期固定加入的 factory class。
 * class 缺失或损坏表示构建装配错误，必须 fail closed，不回退旧 Hub 或第二种 App 路径。
 */
object ManagedCloudAssembly {
    private const val OFFICIAL_FACTORY = "com.muxvia.cloud.OfficialManagedCloudFactory"

    fun create(context: Context): ManagedCloudAdapter {
        val factoryClass = try {
            Class.forName(OFFICIAL_FACTORY)
        } catch (_: ClassNotFoundException) {
            return BrokenOfficialCloudAdapter("Managed cloud factory is missing from the Muxvia App")
        }
        return try {
            val factory = factoryClass.getDeclaredConstructor().newInstance() as? ManagedCloudModuleFactory
                ?: return BrokenOfficialCloudAdapter("Official managed cloud factory has the wrong contract")
            factory.create(context.applicationContext)
        } catch (failure: Throwable) {
            // factory 反射边界必须保留原始异常分类，避免物理设备兼容问题被误报成模块缺失。
            Log.e(TAG, "Official managed cloud module initialization failed", failure)
            BrokenOfficialCloudAdapter("Official managed cloud module could not be loaded")
        }
    }

    private const val TAG = "ManagedCloudAssembly"
}

private class BrokenOfficialCloudAdapter(private val detail: String) : ManagedCloudAdapter {
    override suspend fun routeEligibilityProto(request: ClientBinding.CloudRouteEligibilityRequest): ClientBinding.CloudRouteEligibility = unavailable()
    override suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = unavailable()
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = unavailable()
    override suspend fun currentAccount(): ManagedCloudAccount? = unavailable()
    override suspend fun listDevices(): List<ManagedCloudDevice> = unavailable()
    override suspend fun logout() = unavailable()
    override suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = unavailable()
    override suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = unavailable()
    override suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = unavailable()
    override suspend fun planRouteProto(request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan = unavailable()
    override suspend fun reportQualityProto(request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse = unavailable()
    override suspend fun reportOutcomeProto(request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse = unavailable()

    private fun unavailable(): Nothing {
        throw ManagedEndpointFailure("companion_untrusted", detail)
    }

}
