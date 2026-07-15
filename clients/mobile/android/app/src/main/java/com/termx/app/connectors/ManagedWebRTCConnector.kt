package com.termx.app.connectors

import com.termx.app.managed.*
import com.termx.app.network.BridgeServer
import com.termx.app.transport.WebRTCTransport
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runInterruptible

/**
 * ManagedWebRTCConnector 是 Android App 唯一的 managed endpoint 业务连接器。
 * Cloud adapter 只交换 SDP/ICE；grant 由平台 store 解析并只交给公开 authorizer；成功后才返回可承载业务的 transport。
 */
class ManagedWebRTCConnector(
    private val cloud: ManagedCloudAdapter,
    private val credentials: ClientAccessCredentialStore,
    private val authorizer: ManagedEndpointAuthorizer,
) {
    /** Result 保留实际网络路径或稳定错误码，不暴露 token、SDP credential 或服务端内部套餐细节。 */
    sealed class Result {
        data class Success(
            val transport: WebRTCTransport,
            val observedPath: ObservedPath,
            val routeSelectionReason: ManagedRouteSelectionReason?,
        ) : Result()
        data class Failure(val code: String) : Result()
    }

    /** connect 严格按 resolve、signaling、ICE/DTLS、端到端授权顺序建立单个 endpoint session。 */
    suspend fun connect(
        spec: ManagedEndpointSpec,
        bridge: BridgeServer?,
        onProgress: ((ManagedEndpointPhase) -> Unit)? = null,
    ): Result {
        var transport: WebRTCTransport? = null
        return try {
            ManagedEndpointContract.validate(spec)
            val policy = ManagedEndpointContract.dialPolicy(spec.relayMode)
            val credential = credentials.resolve(spec.grantRef)
            if (!credential.ready()) throw ManagedEndpointFailure("unauthenticated", "client access credential is awaiting pairing")
            if (credential.identity.endpointId != spec.endpointId) {
                throw ManagedEndpointFailure("identity_conflict", "client access credential belongs to another endpoint")
            }
            onProgress?.invoke(ManagedEndpointPhase.RESOLVING)
            val resolution = cloud.resolve(spec)
            if (resolution.managedSessionId.isBlank() || resolution.managedSessionId != resolution.managedSessionId.trim() ||
                (resolution.targetDeviceId.isNotBlank() && resolution.targetDeviceId != spec.targetDeviceId)) {
                throw ManagedEndpointFailure("protocol", "cloud resolved a different or invalid target")
            }
            // smart_route 的候选、评分和成本只属于私有 Planner；公开 App 只执行已选中的短期 ICE 计划。
            val routePlan = if (policy.routePreference == "smart_route") {
                cloud.planManagedRoute(spec, resolution, policy).also { plan ->
                    plan.validate(spec, resolution, System.currentTimeMillis() / 1000L)
                }
            } else {
                null
            }
            val current = WebRTCTransport(bridge, spec.endpointId)
            transport = current
            onProgress?.invoke(ManagedEndpointPhase.SIGNALING)
            val offer = runInterruptible(Dispatchers.IO) {
                current.startManaged(routePlan?.iceServers ?: resolution.iceServers, routePlan?.relayOnly ?: policy.relayOnly)
            } ?: throw ManagedEndpointFailure("protocol", current.lastFailureReason ?: "offer failed")
            val answer = cloud.createSignalingSession(spec, resolution, offer, policy)
            onProgress?.invoke(ManagedEndpointPhase.CONNECTING)
            val connected = runInterruptible(Dispatchers.IO) { current.finishManaged(answer) }
            if (!connected) throw ManagedEndpointFailure("route_unavailable", current.lastFailureReason ?: "WebRTC failed")
            val observedPath = current.currentObservedPath()
                ?: if (routePlan == null) {
                    if (current.currentRelayInUse()) ObservedPath.SINGLE_RELAY else ObservedPath.DIRECT
                } else {
                    throw ManagedEndpointFailure("protocol", "managed route plan ICE path is unavailable")
                }
            if (routePlan != null && routePlan.selectedPath != observedPath) {
                throw ManagedEndpointFailure(
                    "protocol",
                    "managed route plan selected ${routePlan.selectedPath.wireName} but ICE established ${observedPath.wireName}",
                )
            }
            onProgress?.invoke(ManagedEndpointPhase.AUTHORIZING)
            authorizer.authorize(current, spec, credential)
            val qualityReporter = ManagedPathQualityReporter(cloud, resolution.managedSessionId, current)
            current.addBeforeCloseListener(qualityReporter::stop)
            qualityReporter.start()
            transport = null
            Result.Success(current, observedPath, routePlan?.selectionReason)
        } catch (cancelled: CancellationException) {
            transport?.disconnect()
            throw cancelled
        } catch (failure: ManagedEndpointFailure) {
            transport?.disconnect()
            Result.Failure(failure.code)
        } catch (_: Exception) {
            transport?.disconnect()
            Result.Failure("temporary")
        }
    }

    companion object {
        /** community 返回 fail-closed 官方 cloud 缺失实现，不访问旧 Hub/session-token API。 */
        fun community(): ManagedWebRTCConnector = ManagedWebRTCConnector(
            CommunityCloudAdapter(), CommunityClientAccessCredentialStore(), CommunityEndpointAuthorizer(),
        )
    }
}
