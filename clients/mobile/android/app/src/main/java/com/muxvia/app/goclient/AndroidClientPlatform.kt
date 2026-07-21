package com.muxvia.app.goclient

import android.content.Context
import com.google.protobuf.ByteString
import com.muxvia.app.managed.AndroidClientAccessCredentialStore
import com.muxvia.app.managed.ManagedCloudAdapter
import com.muxvia.app.managed.ManagedCloudAssembly
import com.muxvia.app.managed.ManagedEndpointFailure
import kotlinx.coroutines.runBlocking
import muxvia.api.v1.Common
import muxvia.client.binding.v1.ClientBinding
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.atomic.AtomicBoolean

/**
 * AndroidClientPlatform 是 Go Client Engine 的 Android primitive adapter。
 * 它只处理 bindingpb PlatformRequest；连接、认证、Hello、API、generation 和重连真值全部留在 Go。
 */
class AndroidClientPlatform(
    context: Context,
    private val engineHandle: Long,
    private val cloud: ManagedCloudAdapter = ManagedCloudAssembly.create(context.applicationContext),
    private val credentials: AndroidClientAccessCredentialStore = AndroidClientAccessCredentialStore(context.applicationContext),
    private val sshCredentials: AndroidSSHCredentialStore = AndroidSSHCredentialStore(),
    private val endpointRegistry: AndroidEndpointRegistryStore = AndroidEndpointRegistryStore(context.applicationContext),
) : AutoCloseable {
    private val active = AtomicBoolean(true)
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "muxvia-go-platform").apply { isDaemon = true }
    }
    private val pump: Future<*> = executor.submit(::runPump)

    override fun close() {
        if (!active.compareAndSet(true, false)) return
        executor.shutdownNow()
        runCatching { pump.get() }
    }

    private fun runPump() {
        while (active.get()) {
            val payload = try {
                GoClientNative.nextPlatformRequest(engineHandle, 0)
            } catch (_: IllegalStateException) {
                return
            }
            val request = try {
                ClientBinding.PlatformRequest.parseFrom(payload)
            } catch (_: Exception) {
                return
            }
            val response = dispatch(request)
            try {
                GoClientNative.completePlatformRequest(engineHandle, response.toByteArray())
            } catch (_: IllegalStateException) {
                return
            }
        }
    }

    private fun dispatch(request: ClientBinding.PlatformRequest): ClientBinding.PlatformResponse {
        val response = ClientBinding.PlatformResponse.newBuilder().setRequestId(request.requestId)
        return try {
            when (request.requestCase) {
                ClientBinding.PlatformRequest.RequestCase.CREDENTIAL_PREPARE ->
                    response.setCredential(credentials.prepareRecord(
                        request.credentialPrepare.credentialRef,
                        request.credentialPrepare.endpointId,
                    ))
                ClientBinding.PlatformRequest.RequestCase.CREDENTIAL_RESOLVE ->
                    response.setCredential(credentials.resolveRecord(
                        request.credentialResolve.credentialRef,
                        request.credentialResolve.endpointId,
                    ))
                ClientBinding.PlatformRequest.RequestCase.CREDENTIAL_DELETE -> {
                    credentials.delete(request.credentialDelete.credentialRef)
                }
                ClientBinding.PlatformRequest.RequestCase.CREDENTIAL_SIGN ->
                    response.setCredentialSign(ClientBinding.CredentialSignResponse.newBuilder()
                        .setSignature(ByteString.copyFrom(credentials.sign(
                            request.credentialSign.credentialRef,
                            request.credentialSign.payload.toByteArray(),
                        ))))
                ClientBinding.PlatformRequest.RequestCase.CREDENTIAL_BIND ->
                    response.setCredential(credentials.bindRecord(
                        request.credentialBind.credentialRef,
                        request.credentialBind.endpointId,
                        request.credentialBind.capabilityGrant,
                    ))
                ClientBinding.PlatformRequest.RequestCase.ENDPOINT_REGISTRY_LOAD ->
                    response.setEndpointRegistry(ClientBinding.EndpointRegistryLoaded.newBuilder()
                        .setRegistryProto(ByteString.copyFrom(endpointRegistry.load())))
                ClientBinding.PlatformRequest.RequestCase.ENDPOINT_REGISTRY_STORE -> {
                    endpointRegistry.store(
                        request.endpointRegistryStore.registryProto.toByteArray(),
                        request.endpointRegistryStore.deleteCredentialRefsList,
                        credentials,
                        sshCredentials,
                    )
                }
                ClientBinding.PlatformRequest.RequestCase.SSH_CREDENTIAL_LOOKUP ->
                    response.setSshCredential(sshCredentials.lookup(
                        request.sshCredentialLookup.credentialRef,
                        request.sshCredentialLookup.createIfMissing,
                    ))
                ClientBinding.PlatformRequest.RequestCase.SSH_CREDENTIAL_SIGN ->
                    response.setSshCredentialSign(ClientBinding.SSHCredentialSignResponse.newBuilder()
                        .setSignature(ByteString.copyFrom(sshCredentials.sign(
                            request.sshCredentialSign.credentialRef,
                            request.sshCredentialSign.digest.toByteArray(),
                            request.sshCredentialSign.hash,
                        ))))
                ClientBinding.PlatformRequest.RequestCase.SSH_CREDENTIAL_DELETE ->
                    sshCredentials.delete(request.sshCredentialDelete.credentialRef)
                ClientBinding.PlatformRequest.RequestCase.CLOUD_RESOLVE_ENDPOINT ->
                    response.setCloudResolvedEndpoint(runBlocking { cloud.resolveProto(request.cloudResolveEndpoint) })
                ClientBinding.PlatformRequest.RequestCase.CLOUD_CREATE_SIGNALING ->
                    response.setCloudSignaling(ClientBinding.SignalingEvents.newBuilder()
                        .addAllEvents(runBlocking { cloud.createSignalingProto(request.cloudCreateSignaling) }))
                ClientBinding.PlatformRequest.RequestCase.CLOUD_ACQUIRE_RELAY ->
                    response.setCloudRelayLease(runBlocking { cloud.acquireRelayProto(request.cloudAcquireRelay) })
                ClientBinding.PlatformRequest.RequestCase.CLOUD_PLAN_ROUTE ->
                    response.setCloudRoutePlan(runBlocking { cloud.planRouteProto(request.cloudPlanRoute) })
                ClientBinding.PlatformRequest.RequestCase.CLOUD_REPORT_QUALITY ->
                    response.setCloudQualityReported(runBlocking { cloud.reportQualityProto(request.cloudReportQuality) })
                ClientBinding.PlatformRequest.RequestCase.CLOUD_REPORT_OUTCOME ->
                    response.setCloudOutcomeReported(runBlocking { cloud.reportOutcomeProto(request.cloudReportOutcome) })
                ClientBinding.PlatformRequest.RequestCase.CLOUD_ROUTE_ELIGIBILITY ->
                    response.setCloudRouteEligibility(runBlocking { cloud.routeEligibilityProto(request.cloudRouteEligibility) })
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_OPEN_PEER,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_CREATE_OFFER,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_APPLY_ANSWER,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_WAIT_READY,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_CHANNEL_SEND,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_CHANNEL_THRESHOLD,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_PEER_SNAPSHOT,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_CLOSE_PEER,
                ClientBinding.PlatformRequest.RequestCase.WEBRTC_CLOSE_CHANNEL ->
                    throw ManagedEndpointFailure("protocol", "browser WebRTC primitive reached Android platform")
                ClientBinding.PlatformRequest.RequestCase.REQUEST_NOT_SET ->
                    throw ManagedEndpointFailure("protocol", "platform request payload is missing")
                null -> throw ManagedEndpointFailure("protocol", "platform request case is invalid")
            }
            response.build()
        } catch (failure: ManagedEndpointFailure) {
            response.setError(platformError(failure.code, failure.message ?: failure.code)).build()
        } catch (_: Throwable) {
            response.setError(platformError("temporary", "Android platform request failed")).build()
        }
    }

    private fun platformError(code: String, message: String): Common.ApiError {
        val apiCode = when (code) {
            "protocol" -> Common.ApiErrorCode.API_ERROR_CODE_INVALID_REQUEST
            "unauthenticated", "login_required", "capability_invalid", "capability_expired" ->
                Common.ApiErrorCode.API_ERROR_CODE_UNAUTHORIZED
            "cancelled" -> Common.ApiErrorCode.API_ERROR_CODE_CANCELLED
            "route_unavailable", "temporary", "companion_missing" -> Common.ApiErrorCode.API_ERROR_CODE_UNAVAILABLE
            else -> Common.ApiErrorCode.API_ERROR_CODE_INTERNAL
        }
        return Common.ApiError.newBuilder()
            .setCode(apiCode)
            .setMessage(message)
            .setRetryable(apiCode == Common.ApiErrorCode.API_ERROR_CODE_UNAVAILABLE)
            .setAttempted(true)
            .build()
    }
}

/** AndroidGoClientEngine owns one production Go engine and its platform pump. */
class AndroidGoClientEngine(context: Context) : AutoCloseable {
    val handle: Long = GoClientNative.create()
    private val platform = AndroidClientPlatform(context, handle)
    private val closed = AtomicBoolean(false)

    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        GoClientNative.close(handle)
        platform.close()
    }
}
