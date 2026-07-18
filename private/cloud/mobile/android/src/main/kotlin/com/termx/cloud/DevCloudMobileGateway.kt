package com.termx.cloud

import com.google.protobuf.Message
import com.google.protobuf.Parser
import com.google.protobuf.Descriptors
import com.termx.app.managed.ManagedCloudAccount
import com.termx.app.managed.ManagedCloudClientMetadata
import com.termx.app.managed.ManagedCloudLoginFlow
import com.termx.app.managed.ManagedCloudDevice
import com.termx.app.managed.ManagedEndpointFailure
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.bouncycastle.util.encoders.Base64
import org.json.JSONObject
import termx.cloud.v1.CloudCompanion
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
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
    private val allowPublicHTTP: Boolean = false,
    private val now: () -> Instant = { Instant.now() },
    private val sessionStore: CloudSessionStore = MemoryCloudSessionStore(),
) {
    private val controlOrigin = validateOrigin(controlPlaneURL, allowPublicHTTP)
    private val hubOrigin = validateOrigin(hubURL, allowPublicHTTP)
    private val sessionLock = Mutex()
    @Volatile private var accountSession: AccountSession? = null

    /** beginLogin 只返回短期设备码；账号密码和浏览器 Cookie 不进入 Official App。 */
    suspend fun beginLogin(metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = withContext(Dispatchers.IO) {
        val flow = postProto(
            "$controlOrigin/v1/login/begin",
            CloudCompanion.BeginLoginRequest.newBuilder()
                .setMethod(CloudCompanion.LoginMethod.LOGIN_METHOD_DEVICE_CODE)
                .setClientMetadata(metadata.toProto())
                .build(),
            CloudCompanion.LoginFlow.parser(),
            null,
        )
		flow.toManaged()
    }

    /** claimLogin 用 Web 二维码短码认领 Flow；二维码本身不含 flow ID 或账号 Session。 */
    suspend fun claimLogin(userCode: String, metadata: ManagedCloudClientMetadata): ManagedCloudLoginFlow = withContext(Dispatchers.IO) {
        if (userCode.isBlank()) fail("protocol", "mobile activation code is required")
        postProto(
            "$controlOrigin/v1/login/mobile/claim",
            CloudCompanion.ClaimMobileActivationRequest.newBuilder()
                .setUserCode(userCode.trim().uppercase())
                .setClientMetadata(metadata.toProto())
                .build(),
            CloudCompanion.LoginFlow.parser(),
            null,
        ).toManaged()
    }

    /** completeLogin 兑换浏览器已批准的 flow，并把 edge token 直接写入 Keystore store。 */
    suspend fun completeLogin(flowId: String): ManagedCloudAccount = withContext(Dispatchers.IO) {
        if (flowId.isBlank()) fail("protocol", "cloud login flow is required")
        val session = postSession(
            "$controlOrigin/v1/login/complete",
            CloudCompanion.CompleteLoginRequest.newBuilder().setFlowId(flowId).build(),
        )
        sessionStore.save(session)
        accountSession = session
        session.accountSummary()
    }

    private fun CloudCompanion.LoginFlow.toManaged(): ManagedCloudLoginFlow {
        val verification = URI(verificationUri)
        val loopbackHTTP = verification.scheme == "http" && verification.host in setOf("127.0.0.1", "localhost", "::1")
        val trustedScheme = verification.scheme == "https" || loopbackHTTP || allowPublicHTTP && verification.scheme == "http"
        if (flowId.isBlank() || userCode.isBlank() || expiresAtUnix <= now().epochSecond || pollIntervalMillis !in 1..60_000 || !trustedScheme || verification.host.isNullOrBlank() || verification.userInfo != null) {
            fail("protocol", "Control Plane returned an invalid login flow")
        }
        return ManagedCloudLoginFlow(flowId, verificationUri, userCode, expiresAtUnix, pollIntervalMillis)
    }

    private fun ManagedCloudClientMetadata.toProto(): CloudCompanion.DeviceMetadata =
        CloudCompanion.DeviceMetadata.newBuilder()
            .setDisplayName(displayName)
            .setPlatform(platform)
            .setTermxVersion(termxVersion)
            .build()

    /** currentAccount 只返回仍有效的账号摘要。 */
    suspend fun currentAccount(): ManagedCloudAccount? = sessionLock.withLock {
        accountSession?.takeIf { now().isBefore(it.expiresAt) }?.accountSummary()
            ?: sessionStore.load(now())?.also { accountSession = it }?.accountSummary()
    }

    /** listDevices 从 Hub edge snapshot 读取同账号设备，不访问 Control Plane 或 terminal 数据。 */
    suspend fun listDevices(): List<ManagedCloudDevice> = withContext(Dispatchers.IO) {
        val session = accountSession()
        val response = postEdgeProto(
            "$hubOrigin/v1/devices/list",
            CloudCompanion.ListManagedDevicesRequest.newBuilder().setSchemaVersion(1).build(),
            CloudCompanion.ListManagedDevicesResponse.parser(),
            session,
        )
        response.devicesList.map { device ->
            val kind = when (device.kind) {
                CloudCompanion.ManagedDeviceKind.MANAGED_DEVICE_KIND_CLIENT -> "client"
                CloudCompanion.ManagedDeviceKind.MANAGED_DEVICE_KIND_DAEMON -> "daemon"
                else -> fail("protocol", "Hub returned an invalid managed device kind")
            }
            if (device.deviceId.isBlank() || device.displayName.isBlank() || device.presence == CloudCompanion.PresenceState.PRESENCE_STATE_UNSPECIFIED || kind == "daemon" && device.deviceFingerprint.isBlank()) {
                fail("protocol", "Hub returned an invalid managed device directory")
            }
            ManagedCloudDevice(device.deviceId, device.deviceFingerprint, device.displayName, device.platform, kind, device.presence == CloudCompanion.PresenceState.PRESENCE_STATE_ONLINE, device.revoked)
        }
    }

    /** logout 删除账号 edge session；pairing grant 属于独立 secure store，不在此处清理。 */
    suspend fun logout() = sessionLock.withLock {
        accountSession?.token?.fill(0)
		accountSession?.refreshToken?.fill(0)
        accountSession = null
        sessionStore.clear()
    }

    /** resolveProto 直接转发 Go Client Engine 的 cloudpb request，不建立 Kotlin endpoint/session 真值。 */
    suspend fun resolveProto(request: CloudCompanion.ResolveEndpointRequest): CloudCompanion.ResolvedEndpoint = withContext(Dispatchers.IO) {
        val response = postEdgeProto(
            "$hubOrigin/v1/endpoints/resolve",
            request,
            CloudCompanion.ResolvedEndpoint.parser(),
            accountSession(),
        )
        if (response.endpointId != request.endpointId || response.targetDeviceId != request.targetDeviceId || response.managedSessionId.isBlank()) {
            fail("protocol", "Hub resolved a different managed endpoint")
        }
        response
    }

    /** createSignalingProto 返回完整 signaling Proto event 序列，candidate/order 真值由 Go managed adapter 消费。 */
    suspend fun createSignalingProto(request: CloudCompanion.CreateSignalingSessionRequest): List<CloudCompanion.SignalingEvent> = withContext(Dispatchers.IO) {
        val signaling = postHubStream("$hubOrigin/v1/signaling/create", accountSession(), request)
        val candidates = signaling.candidates.map { candidate ->
            CloudCompanion.SignalingEvent.newBuilder()
                .setCandidate(CloudCompanion.IceCandidate.newBuilder().setCandidate(candidate))
                .build()
        }
        candidates + signaling.finalEvent
    }

    /** acquireRelayProto 保留 Hub 签发的完整 lease；Kotlin 不提取或缓存 TURN material。 */
    suspend fun acquireRelayProto(request: CloudCompanion.AcquireRelayLeaseRequest): CloudCompanion.RelayLease = withContext(Dispatchers.IO) {
        val lease = postEdgeProto(
            "$hubOrigin/v1/relay/leases/acquire",
            request,
            CloudCompanion.RelayLease.parser(),
            accountSession(),
        )
        if (lease.pathKind != CloudCompanion.ObservedPath.OBSERVED_PATH_SINGLE_RELAY || lease.expiresAtUnix <= now().epochSecond || lease.iceServersCount != 1) {
            fail("protocol", "Hub returned invalid Relay material")
        }
        lease
    }

    suspend fun planRouteProto(@Suppress("UNUSED_PARAMETER") request: CloudCompanion.PlanManagedRouteRequest): CloudCompanion.ManagedRoutePlan =
        fail("route_unavailable", "dev cloud SmartRoute is not enabled")

    suspend fun reportQualityProto(@Suppress("UNUSED_PARAMETER") request: CloudCompanion.ReportPathQualityRequest): CloudCompanion.ReportPathQualityResponse =
        fail("route_unavailable", "dev cloud path quality reporting is not enabled")

    suspend fun reportOutcomeProto(@Suppress("UNUSED_PARAMETER") request: CloudCompanion.ReportConnectionOutcomeRequest): CloudCompanion.ReportConnectionOutcomeResponse =
        fail("route_unavailable", "dev cloud connection outcome reporting is not enabled")

    private suspend fun accountSession(): AccountSession = sessionLock.withLock {
		val currentTime = now()
		val current = accountSession ?: sessionStore.loadRefreshable(currentTime)
		if (current != null && current.expiresAt.epochSecond - currentTime.epochSecond > REFRESH_WINDOW_SECONDS) {
			accountSession = current
			return@withLock current
		}
		if (current != null && currentTime.isBefore(current.refreshExpiresAt)) {
			try {
				val refreshed = refreshSession(current)
				sessionStore.save(refreshed)
				current.token.fill(0)
				current.refreshToken.fill(0)
				accountSession = refreshed
				return@withLock refreshed
			} catch (failure: ManagedEndpointFailure) {
				if (currentTime.isBefore(current.expiresAt)) {
					accountSession = current
					return@withLock current
				}
				throw failure
			}
		}
		fail("login_required", "Official mobile cloud account login is required")
    }

    private fun postSession(endpoint: String, request: Message): AccountSession {
        return withConnection(endpoint, "application/x-protobuf", null) { connection ->
            writeRequest(connection, request.toByteArray())
            val payload = readSuccess(connection, "application/json")
            val json = JSONObject(String(payload, Charsets.UTF_8))
			decodeAccountSession(json)
        }
    }

	private fun refreshSession(current: AccountSession): AccountSession {
		val payload = JSONObject()
			.put("kind", "account")
			.put("refresh_token", encodeBase64(current.refreshToken))
		return withConnection("$controlOrigin/v1/sessions/refresh", "application/json", null) { connection ->
			writeRequest(connection, payload.toString().toByteArray(Charsets.UTF_8))
			val response = JSONObject(String(readSuccess(connection, "application/json"), Charsets.UTF_8))
			decodeAccountSession(response)
		}
	}

	private fun decodeAccountSession(json: JSONObject): AccountSession {
		requireKeys(json, setOf("kind", "account_id", "account_label", "device_id", "expires_at_unix", "access_token", "refresh_token", "refresh_expires_at_unix", "hub_id", "hub_url", "hub_region", "hub_directory_version"), "cloud session")
		if (json.requiredString("kind") != "account") fail("protocol", "Control Plane returned a non-account session")
		if (json.requiredString("account_id").isBlank() || json.requiredString("account_label").isBlank() || json.requiredString("device_id").isBlank()) {
			fail("protocol", "Control Plane returned incomplete account identity")
		}
		val token = decodeBase64(json.requiredString("access_token"))
		val refreshToken = decodeBase64(json.requiredString("refresh_token"))
		val expiresAt = Instant.ofEpochSecond(json.requiredLong("expires_at_unix"))
		val refreshExpiresAt = Instant.ofEpochSecond(json.requiredLong("refresh_expires_at_unix"))
		val hubId = json.requiredString("hub_id")
		val signedHubURL = json.requiredString("hub_url")
		val region = json.requiredString("hub_region")
		val directoryVersion = json.requiredLong("hub_directory_version")
		if (token.size < 16 || refreshToken.size < 32 || !now().isBefore(expiresAt) || !refreshExpiresAt.isAfter(expiresAt) || hubId.isBlank() || signedHubURL.isBlank() || region.isBlank() || directoryVersion <= 0) fail("login_required", "Control Plane account session is expired")
		return AccountSession(token, expiresAt, refreshToken, refreshExpiresAt, json.requiredString("account_id"), json.requiredString("account_label"), json.requiredString("device_id"), hubId, signedHubURL, region, directoryVersion)
	}

    private fun postHubStream(endpoint: String, session: AccountSession, request: Message): HubSignalingResult {
        val envelope = edgeEnvelope(session, request)
        return withConnection(endpoint, "application/json", session.token) { connection ->
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
        }
    }

    private fun <T : Message> postEdgeProto(endpoint: String, request: Message, parser: Parser<T>, session: AccountSession): T {
        return withConnection(endpoint, "application/json", session.token) { connection ->
            writeRequest(connection, edgeEnvelope(session, request).toString().toByteArray(Charsets.UTF_8))
            val payload = readSuccess(connection, "application/x-protobuf")
            val response = parser.parseFrom(payload)
            if (messageHasUnknown(response)) fail("protocol", "Hub response contains unknown fields")
            response
        }
    }

    private fun edgeEnvelope(session: AccountSession, request: Message): JSONObject = JSONObject()
        .put("account_id", session.accountId)
        .put("device_id", session.deviceId)
        .put("payload", encodeBase64(request.toByteArray()))

    private fun <T : Message> postProto(endpoint: String, request: Message, parser: Parser<T>, token: ByteArray?): T {
        return withConnection(endpoint, "application/x-protobuf", token) { connection ->
            writeRequest(connection, request.toByteArray())
            val payload = readSuccess(connection, "application/x-protobuf")
            val response = parser.parseFrom(payload)
            if (messageHasUnknown(response)) fail("protocol", "cloud response contains unknown fields")
            response
        }
    }

    /**
     * withConnection 是移动 Cloud HTTP 的统一失败边界：服务端结构化错误保持原码，设备网络失败不得泄漏成无语义异常。
     * 它不做重试或 fallback；调用方决定用户是否重新发起激活、解析或信令动作。
     */
    private inline fun <T> withConnection(
        endpoint: String,
        contentType: String,
        token: ByteArray?,
        request: (HttpURLConnection) -> T,
    ): T {
        var connection: HttpURLConnection? = null
        try {
            connection = open(endpoint, contentType, token)
            return request(connection)
        } catch (failure: ManagedEndpointFailure) {
            throw failure
        } catch (_: SocketTimeoutException) {
            fail("temporary", "TermX Cloud request timed out. Check the phone network and try again.")
        } catch (_: IOException) {
            fail("route_unavailable", "TermX Cloud is unreachable from this phone. Check Wi-Fi or mobile data and try again.")
        } finally {
            connection?.disconnect()
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

    private data class HubSignalingResult(
        val finalEvent: CloudCompanion.SignalingEvent,
        val candidates: List<String>,
    )

    private companion object {
        const val MAX_BODY_BYTES = 4 shl 20
        const val MAX_SIGNAL_CANDIDATES = 256
		const val REFRESH_WINDOW_SECONDS = 15 * 60L

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

private fun AccountSession.accountSummary(): ManagedCloudAccount =
    ManagedCloudAccount(accountId, accountLabel, expiresAt.epochSecond)
