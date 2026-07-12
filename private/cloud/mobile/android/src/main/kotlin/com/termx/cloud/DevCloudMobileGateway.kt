package com.termx.cloud

import com.google.protobuf.Message
import com.google.protobuf.Parser
import com.google.protobuf.Descriptors
import com.termx.app.managed.ManagedDialPolicy
import com.termx.app.managed.ManagedEndpointFailure
import com.termx.app.managed.ManagedEndpointResolution
import com.termx.app.managed.ManagedEndpointSpec
import com.termx.app.managed.ManagedIceServer
import com.termx.app.managed.ManagedPathQualitySummary
import com.termx.app.managed.ManagedRoutePlan
import com.termx.app.managed.ManagedSignalAnswer
import com.termx.app.managed.ManagedSignalOffer
import com.termx.app.managed.RelayMode
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.bouncycastle.util.encoders.Base64
import org.json.JSONObject
import termx.cloud.v1.CloudCompanion
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.time.Instant

/**
 * DevCloudMobileGateway 是 Official development APK 到 CLOUD002 dev-local Control Plane/Hub 的真实 HTTP adapter。
 * 它只持有账号 cloud session、admission、SDP/ICE；CapabilityGrant、DeviceIdentity private key 和 terminal payload 不进入本类。
 */
internal class DevCloudMobileGateway(
    controlPlaneURL: String,
    hubURL: String,
    allowPublicHTTP: Boolean = false,
    private val now: () -> Instant = { Instant.now() },
) {
    private val controlOrigin = validateOrigin(controlPlaneURL, allowPublicHTTP)
    private val hubOrigin = validateOrigin(hubURL, allowPublicHTTP)
    private val sessionLock = Mutex()
    @Volatile private var accountSession: AccountSession? = null

    /** resolve 创建独立 ManagedSession；relay-only 时额外领取 caller-specific 短期 TURN material。 */
    suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution = withContext(Dispatchers.IO) {
        val session = accountSession()
        val request = CloudCompanion.ResolveEndpointRequest.newBuilder()
            .setEndpointId(spec.endpointId)
            .setTargetDeviceId(spec.targetDeviceId)
            .build()
        val response = postProto(
            "$controlOrigin/v1/endpoints/resolve",
            request,
            CloudCompanion.ResolvedEndpoint.parser(),
            session.token,
        )
        if (response.endpointId != spec.endpointId || response.targetDeviceId != spec.targetDeviceId || response.managedSessionId.isBlank()) {
            fail("protocol", "Control Plane resolved a different managed endpoint")
        }
        if (response.presence != CloudCompanion.PresenceState.PRESENCE_STATE_ONLINE) {
            fail("route_unavailable", "managed daemon is offline")
        }
        val iceServers = if (spec.relayMode == RelayMode.RELAY_ONLY) {
            acquireRelayLease(session, response.managedSessionId, spec.targetDeviceId)
        } else {
            response.iceServersList.map(::iceServer)
        }
        ManagedEndpointResolution(response.managedSessionId, response.targetDeviceId, iceServers)
    }

    /** createSignalingSession 先取得 ManagedSession-bound client admission，再读取 Hub answer stream。 */
    suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer = withContext(Dispatchers.IO) {
        val session = accountSession()
        val request = CloudCompanion.CreateSignalingSessionRequest.newBuilder()
            .setEndpointId(spec.endpointId)
            .setManagedSessionId(resolution.managedSessionId)
            .setTargetDeviceId(spec.targetDeviceId)
            .setOfferSdp(offer.sdp)
            .setRoutePreference(routePreference(policy.routePreference))
            .setRelayOnly(policy.relayOnly)
            .build()
        val admission = postAdmission("$controlOrigin/v1/admissions/client", request, session.token)
        if (admission.sessionId != resolution.managedSessionId || admission.targetDeviceId != spec.targetDeviceId || admission.sessionKind != "managed") {
            fail("protocol", "Control Plane returned an invalid client admission")
        }
        val signaling = postHubStream("$hubOrigin/v1/signaling/create", admission, request)
        val event = signaling.finalEvent
        when (event.payloadCase) {
            CloudCompanion.SignalingEvent.PayloadCase.ANSWER -> {
                val answer = event.answer
                if (answer.signalingSessionId.isBlank() || answer.sdp.isBlank()) fail("protocol", "Hub returned an invalid WebRTC answer")
                val candidates = (signaling.candidates + answer.candidatesList.map { it.candidate.trim() })
                    .filter(String::isNotBlank)
                    .distinct()
                ManagedSignalAnswer(answer.sdp, candidates)
            }
            CloudCompanion.SignalingEvent.PayloadCase.ERROR -> throw cloudFailure(event.error)
            CloudCompanion.SignalingEvent.PayloadCase.CLOSED -> fail("route_unavailable", "Hub closed managed signaling")
            else -> fail("protocol", "Hub returned an unsupported signaling event")
        }
    }

    /** reportPathQuality 保持 CLOUD004 dev contract 的 measurement sink 未启用语义，不影响当前 transport。 */
    suspend fun reportPathQuality(@Suppress("UNUSED_PARAMETER") summary: ManagedPathQualitySummary) {
        fail("route_unavailable", "dev cloud path quality reporting is not enabled")
    }

    /** planManagedRoute 保持 SmartRoute deferred；CLOUD005 只验证 direct 与显式 relay-only。 */
    suspend fun planManagedRoute(
        @Suppress("UNUSED_PARAMETER") spec: ManagedEndpointSpec,
        @Suppress("UNUSED_PARAMETER") resolution: ManagedEndpointResolution,
        @Suppress("UNUSED_PARAMETER") policy: ManagedDialPolicy,
    ): ManagedRoutePlan = fail("route_unavailable", "dev cloud SmartRoute is not enabled")

    private suspend fun accountSession(): AccountSession = sessionLock.withLock {
        accountSession?.takeIf { now().isBefore(it.expiresAt) }?.let { return@withLock it }
        val flow = postProto(
            "$controlOrigin/v1/login/begin",
            CloudCompanion.BeginLoginRequest.newBuilder().setMethod(CloudCompanion.LoginMethod.LOGIN_METHOD_DEVICE_CODE).build(),
            CloudCompanion.LoginFlow.parser(),
            null,
        )
        if (flow.flowId.isBlank() || flow.expiresAtUnix <= now().epochSecond) fail("login_required", "dev cloud login flow is unavailable")
        val session = postSession(
            "$controlOrigin/v1/login/complete",
            CloudCompanion.CompleteLoginRequest.newBuilder().setFlowId(flow.flowId).build(),
        )
        accountSession = session
        session
    }

    private fun acquireRelayLease(session: AccountSession, managedSessionId: String, targetDeviceId: String): List<ManagedIceServer> {
        val lease = postProto(
            "$controlOrigin/v1/relay/leases/acquire",
            CloudCompanion.AcquireRelayLeaseRequest.newBuilder()
                .setManagedSessionId(managedSessionId)
                .setTargetDeviceId(targetDeviceId)
                .setRoutePreference(CloudCompanion.RoutePreference.ROUTE_PREFERENCE_STANDARD_RELAY)
                .build(),
            CloudCompanion.RelayLease.parser(),
            session.token,
        )
        if (lease.pathKind != CloudCompanion.ObservedPath.OBSERVED_PATH_SINGLE_RELAY || lease.expiresAtUnix <= now().epochSecond || lease.iceServersCount != 1) {
            fail("protocol", "Control Plane returned invalid Relay material")
        }
        return lease.iceServersList.map(::iceServer)
    }

    private fun postSession(endpoint: String, request: Message): AccountSession {
        val connection = open(endpoint, "application/x-protobuf", null)
        return try {
            writeRequest(connection, request.toByteArray())
            val payload = readSuccess(connection, "application/json")
            val json = JSONObject(String(payload, Charsets.UTF_8))
            requireKeys(json, setOf("kind", "account_id", "account_label", "device_id", "expires_at_unix", "access_token"), "cloud session")
            if (json.requiredString("kind") != "account") fail("protocol", "Control Plane returned a non-account session")
            if (json.requiredString("account_id").isBlank() || json.requiredString("account_label").isBlank() || json.requiredString("device_id").isBlank()) {
                fail("protocol", "Control Plane returned incomplete account identity")
            }
            val token = decodeBase64(json.requiredString("access_token"))
            val expiresAt = Instant.ofEpochSecond(json.requiredLong("expires_at_unix"))
            if (token.size < 16 || !now().isBefore(expiresAt)) fail("login_required", "Control Plane account session is expired")
            AccountSession(token, expiresAt)
        } finally {
            connection.disconnect()
        }
    }

    private fun postAdmission(endpoint: String, request: Message, token: ByteArray): Admission {
        val connection = open(endpoint, "application/x-protobuf", token)
        return try {
            writeRequest(connection, request.toByteArray())
            val payload = readSuccess(connection, "application/json")
            val json = JSONObject(String(payload, Charsets.UTF_8))
            requireKeys(json, setOf(
                "reference", "hub_id", "account_id", "device_id", "target_device_id", "session_kind",
                "session_id", "expires_at_unix", "ticket",
            ), "Hub admission")
            val admission = Admission(
                reference = json.requiredString("reference"),
                hubId = json.requiredString("hub_id"),
                accountId = json.requiredString("account_id"),
                deviceId = json.requiredString("device_id"),
                targetDeviceId = json.optionalString("target_device_id"),
                sessionKind = json.requiredString("session_kind"),
                sessionId = json.requiredString("session_id"),
                expiresAtUnix = json.requiredLong("expires_at_unix"),
                ticket = decodeBase64(json.requiredString("ticket")),
            )
            if (admission.reference.isBlank() || admission.hubId.isBlank() || admission.accountId.isBlank() || admission.deviceId.isBlank() ||
                admission.sessionId.isBlank() || admission.ticket.size < 16 || admission.expiresAtUnix <= now().epochSecond) {
                fail("protocol", "Control Plane returned an invalid Hub admission")
            }
            admission
        } finally {
            connection.disconnect()
        }
    }

    private fun postHubStream(endpoint: String, admission: Admission, request: Message): HubSignalingResult {
        val envelope = JSONObject().put("admission", admission.toJson()).put("payload", encodeBase64(request.toByteArray()))
        val connection = open(endpoint, "application/json", null)
        return try {
            writeRequest(connection, envelope.toString().toByteArray(Charsets.UTF_8))
            val status = connection.responseCode
            if (status !in 200..299) throw readCloudFailure(connection)
            if (connection.contentType?.substringBefore(';') != "application/x-termx-cloud-stream") {
                fail("protocol", "Hub returned an invalid stream media type")
            }
            val input = DataInputStream(connection.inputStream.buffered())
            var result: CloudCompanion.SignalingEvent? = null
            val candidates = mutableListOf<String>()
            while (result == null) {
                val size = try { input.readInt() } catch (_: Exception) { fail("route_unavailable", "Hub signaling stream ended before answer") }
                if (size <= 0 || size > MAX_BODY_BYTES) fail("protocol", "Hub signaling frame size is invalid")
                val data = ByteArray(size)
                input.readFully(data)
                val event = CloudCompanion.SignalingEvent.parseFrom(data)
                if (messageHasUnknown(event)) fail("protocol", "Hub signaling event contains unknown fields")
                if (event.payloadCase == CloudCompanion.SignalingEvent.PayloadCase.CANDIDATE) {
                    val candidate = event.candidate.candidate.trim()
                    if (candidate.isEmpty() || candidates.size >= MAX_SIGNAL_CANDIDATES) {
                        fail("protocol", "Hub signaling candidate stream is invalid")
                    }
                    candidates += candidate
                } else {
                    result = event
                }
            }
            HubSignalingResult(result, candidates)
        } finally {
            connection.disconnect()
        }
    }

    private fun <T : Message> postProto(endpoint: String, request: Message, parser: Parser<T>, token: ByteArray?): T {
        val connection = open(endpoint, "application/x-protobuf", token)
        return try {
            writeRequest(connection, request.toByteArray())
            val payload = readSuccess(connection, "application/x-protobuf")
            val response = parser.parseFrom(payload)
            if (messageHasUnknown(response)) fail("protocol", "cloud response contains unknown fields")
            response
        } finally {
            connection.disconnect()
        }
    }

    private fun open(endpoint: String, contentType: String, token: ByteArray?): HttpURLConnection {
        val connection = URL(endpoint).openConnection() as HttpURLConnection
        connection.requestMethod = "POST"
        connection.instanceFollowRedirects = false
        connection.connectTimeout = 8_000
        connection.readTimeout = 30_000
        connection.doOutput = true
        connection.setRequestProperty("Content-Type", contentType)
        connection.setRequestProperty("Accept", "application/x-protobuf, application/json, application/x-termx-cloud-stream")
        if (token != null) connection.setRequestProperty("Authorization", "Bearer ${encodeBase64Url(token)}")
        return connection
    }

    private fun writeRequest(connection: HttpURLConnection, payload: ByteArray) {
        if (payload.size > MAX_BODY_BYTES) fail("protocol", "cloud request is too large")
        connection.setFixedLengthStreamingMode(payload.size)
        connection.outputStream.use { it.write(payload) }
    }

    private fun readSuccess(connection: HttpURLConnection, mediaType: String): ByteArray {
        val status = connection.responseCode
        if (status !in 200..299) throw readCloudFailure(connection)
        if (connection.contentType?.substringBefore(';') != mediaType) fail("protocol", "cloud response media type is invalid")
        return connection.inputStream.use(::readLimited)
    }

    private fun readCloudFailure(connection: HttpURLConnection): ManagedEndpointFailure {
        val payload = (connection.errorStream ?: connection.inputStream).use(::readLimited)
        return try {
            cloudFailure(CloudCompanion.CloudError.parseFrom(payload))
        } catch (_: Exception) {
            ManagedEndpointFailure("protocol", "cloud service returned an invalid error")
        }
    }

    private fun readLimited(input: java.io.InputStream): ByteArray {
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(8192)
        var total = 0
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            total += count
            if (total > MAX_BODY_BYTES) fail("protocol", "cloud response is too large")
            output.write(buffer, 0, count)
        }
        return output.toByteArray()
    }

    private fun cloudFailure(error: CloudCompanion.CloudError): ManagedEndpointFailure {
        val code = when (error.code) {
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_LOGIN_REQUIRED -> "login_required"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_DEVICE_NOT_FOUND -> "device_not_found"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_UNAUTHENTICATED -> "unauthenticated"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_ENTITLEMENT_DENIED -> "entitlement_denied"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_QUOTA_EXHAUSTED -> "quota_exhausted"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE,
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_REGION_UNAVAILABLE -> "route_unavailable"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_BACKPRESSURE -> "backpressure"
            CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_PROTOCOL -> "protocol"
            else -> "temporary"
        }
        return ManagedEndpointFailure(code, error.message.ifBlank { "managed cloud request failed" })
    }

    private fun routePreference(value: String): CloudCompanion.RoutePreference = when (value) {
        "direct_only" -> CloudCompanion.RoutePreference.ROUTE_PREFERENCE_DIRECT_ONLY
        "standard_relay" -> CloudCompanion.RoutePreference.ROUTE_PREFERENCE_STANDARD_RELAY
        "smart_route" -> CloudCompanion.RoutePreference.ROUTE_PREFERENCE_SMART_ROUTE
        else -> fail("protocol", "managed route preference is invalid")
    }

    private fun iceServer(server: CloudCompanion.IceServer): ManagedIceServer {
        if (server.urlsCount == 0) fail("protocol", "cloud ICE server is empty")
        return ManagedIceServer(server.urlsList, server.username, server.credential)
    }

    private data class AccountSession(val token: ByteArray, val expiresAt: Instant)

    private data class HubSignalingResult(
        val finalEvent: CloudCompanion.SignalingEvent,
        val candidates: List<String>,
    )

    private data class Admission(
        val reference: String,
        val hubId: String,
        val accountId: String,
        val deviceId: String,
        val targetDeviceId: String,
        val sessionKind: String,
        val sessionId: String,
        val expiresAtUnix: Long,
        val ticket: ByteArray,
    ) {
        fun toJson(): JSONObject = JSONObject()
            .put("reference", reference)
            .put("hub_id", hubId)
            .put("account_id", accountId)
            .put("device_id", deviceId)
            .apply { if (targetDeviceId.isNotEmpty()) put("target_device_id", targetDeviceId) }
            .put("session_kind", sessionKind)
            .put("session_id", sessionId)
            .put("expires_at_unix", expiresAtUnix)
            .put("ticket", encodeBase64(ticket))
    }

    private companion object {
        const val MAX_BODY_BYTES = 4 shl 20
        const val MAX_SIGNAL_CANDIDATES = 256

        fun validateOrigin(value: String, allowPublicHTTP: Boolean): String {
            val uri = try { URI(value.trim()) } catch (_: Exception) { fail("protocol", "dev cloud origin is invalid") }
            val loopback = uri.host in setOf("127.0.0.1", "localhost", "::1")
            if (uri.scheme != "http" || uri.userInfo != null || uri.path !in listOf("", null) || uri.query != null || uri.fragment != null ||
                (!loopback && !allowPublicHTTP) || uri.host.isNullOrBlank() || uri.port !in 1..65535) {
                fail("protocol", "dev cloud origin is not allowed by the selected staging profile")
            }
            return value.trim().trimEnd('/')
        }

        fun encodeBase64(value: ByteArray): String = Base64.toBase64String(value)

        fun encodeBase64Url(value: ByteArray): String = encodeBase64(value).trimEnd('=').replace('+', '-').replace('/', '_')

        fun decodeBase64(value: String): ByteArray = try { Base64.decode(value) } catch (_: Exception) { fail("protocol", "cloud secret encoding is invalid") }

        fun requireKeys(value: JSONObject, allowed: Set<String>, label: String) {
            if (!allowed.containsAll(value.keys().asSequence().toSet())) fail("protocol", "$label contains unknown fields")
        }

        fun JSONObject.requiredString(key: String): String {
            if (!has(key) || get(key) !is String) fail("protocol", "cloud response $key is invalid")
            return getString(key).trim()
        }

        fun JSONObject.optionalString(key: String): String {
            if (!has(key)) return ""
            if (get(key) !is String) fail("protocol", "cloud response $key is invalid")
            return getString(key).trim()
        }

        fun JSONObject.requiredLong(key: String): Long {
            if (!has(key)) fail("protocol", "cloud response $key is invalid")
            val value = get(key)
            if (value !is Int && value !is Long) fail("protocol", "cloud response $key is invalid")
            return (value as Number).toLong()
        }

        fun messageHasUnknown(message: Message): Boolean {
            if (message.unknownFields.asMap().isNotEmpty()) return true
            for ((field, value) in message.allFields) {
                if (field.javaType != Descriptors.FieldDescriptor.JavaType.MESSAGE) continue
                if (field.isRepeated) {
                    @Suppress("UNCHECKED_CAST")
                    val children = value as List<Message>
                    if (children.any(::messageHasUnknown)) return true
                } else if (messageHasUnknown(value as Message)) {
                    return true
                }
            }
            return false
        }

        fun fail(code: String, message: String): Nothing = throw ManagedEndpointFailure(code, message)
    }
}
