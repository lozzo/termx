package com.termx.cloud

import com.termx.app.managed.ManagedEndpointContract
import com.termx.app.managed.ManagedEndpointFailure
import com.termx.app.managed.ManagedEndpointSpec
import com.termx.app.managed.ManagedSignalOffer
import com.termx.app.managed.RelayMode
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import termx.cloud.v1.CloudCompanion
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketException
import java.nio.charset.StandardCharsets
import java.time.Instant
import java.util.Base64
import java.util.Collections
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread

/** DevCloudMobileGatewayTest 证明 Official Android 使用真实 dev Control Plane/Hub wire contract，而非占位 gateway。 */
class DevCloudMobileGatewayTest {
    private val now = Instant.parse("2026-07-11T12:00:00Z")

    @Test
    fun resolvesAndSignalsThroughLoopbackCloudWithOnePrivateAccountSession() = runBlocking {
        val records = Collections.synchronizedList(mutableListOf<RecordedRequest>())
        val accessToken = ByteArray(32) { 0x31 }
        val bearer = "Bearer ${Base64.getUrlEncoder().withoutPadding().encodeToString(accessToken)}"

        val hub = TestHttpServer { request ->
            records += request
            val envelope = JSONObject(String(request.body, Charsets.UTF_8))
            assertEquals("account-1", envelope.getString("account_id"))
            assertEquals("android-client-1", envelope.getString("device_id"))
            assertEquals(bearer, request.authorization)
            when (request.path) {
                "/v1/endpoints/resolve" -> {
                    val parsed = CloudCompanion.ResolveEndpointRequest.parseFrom(Base64.getDecoder().decode(envelope.getString("payload")))
                    protoResponse(CloudCompanion.ResolvedEndpoint.newBuilder().setEndpointId(parsed.endpointId).setTargetDeviceId(parsed.targetDeviceId).setPresence(CloudCompanion.PresenceState.PRESENCE_STATE_ONLINE).setHubId("hub-dev-1").setHubUrl("https://hub.example.test").setManagedSessionId("managed-session-1").build().toByteArray())
                }
                "/v1/signaling/create" -> {
                    val signalingRequest = CloudCompanion.CreateSignalingSessionRequest.parseFrom(Base64.getDecoder().decode(envelope.getString("payload")))
                    assertEquals("offer-sdp", signalingRequest.offerSdp)
                    assertFalse(envelope.toString().contains("capability", ignoreCase = true))
                    streamResponse(CloudCompanion.SignalingEvent.newBuilder().setCandidate(CloudCompanion.IceCandidate.newBuilder().setCandidate("candidate:streamed")).build(), CloudCompanion.SignalingEvent.newBuilder().setAnswer(CloudCompanion.SignalingAnswer.newBuilder().setSignalingSessionId("signal-1").setSdp("answer-sdp").addCandidates(CloudCompanion.IceCandidate.newBuilder().setCandidate("candidate:answer"))).build())
                }
                "/v1/relay/leases/acquire" -> protoResponse(CloudCompanion.RelayLease.newBuilder().setLeaseId("lease-1").setSignedLease(com.google.protobuf.ByteString.copyFromUtf8("signed-lease")).setExpiresAtUnix(now.plusSeconds(300).epochSecond).setPathKind(CloudCompanion.ObservedPath.OBSERVED_PATH_SINGLE_RELAY).addIceServers(CloudCompanion.IceServer.newBuilder().addUrls("turn:127.0.0.1:41003?transport=udp").setUsername("relay-user").setCredential("relay-password")).build().toByteArray())
                else -> throw AssertionError("unexpected Hub path ${request.path}")
            }
        }
        val control = TestHttpServer { request ->
            records += request
            when (request.path) {
                "/v1/login/begin" -> {
                    val parsed = CloudCompanion.BeginLoginRequest.parseFrom(request.body)
                    assertEquals(CloudCompanion.LoginMethod.LOGIN_METHOD_DEVICE_CODE, parsed.method)
                    protoResponse(
                        CloudCompanion.LoginFlow.newBuilder()
                            .setFlowId("flow-1")
                            .setVerificationUri("http://127.0.0.1/dev-login")
                            .setUserCode("TERM-X")
                            .setExpiresAtUnix(now.plusSeconds(60).epochSecond)
                            .setPollIntervalMillis(1000)
                            .build()
                            .toByteArray(),
                    )
                }
                "/v1/login/complete" -> {
                    assertEquals("flow-1", CloudCompanion.CompleteLoginRequest.parseFrom(request.body).flowId)
                    jsonResponse(JSONObject()
                        .put("kind", "account")
                        .put("account_id", "account-1")
                        .put("account_label", "TermX development")
                        .put("device_id", "android-client-1")
                        .put("expires_at_unix", now.plusSeconds(3600).epochSecond)
                        .put("hub_id", "hub-dev-1")
                        .put("hub_url", hub.origin)
                        .put("hub_region", "local-1")
                        .put("hub_directory_version", 1)
                        .put("access_token", Base64.getEncoder().encodeToString(accessToken)))
                }
                else -> throw AssertionError("unexpected Control Plane path ${request.path}")
            }
        }

        try {
            val sessionStore = MemoryCloudSessionStore()
            val gateway = DevCloudMobileGateway(control.origin, hub.origin, now = { now }, sessionStore = sessionStore)
            val loginFlow = gateway.beginLogin()
            assertEquals("TERM-X", loginFlow.userCode)
            assertEquals("account-1", gateway.completeLogin(loginFlow.flowId).accountId)
            val spec = ManagedEndpointSpec(
                endpointId = "endpoint-1",
                targetDeviceId = "daemon-1",
                deviceFingerprint = "ed25519-sha256:fixture",
                grantRef = "grant-ref-1",
                relayMode = RelayMode.DIRECT,
            )
            val resolution = gateway.resolve(spec)
            assertEquals("managed-session-1", resolution.managedSessionId)
            control.close()
            val restartedGateway = DevCloudMobileGateway(control.origin, hub.origin, now = { now }, sessionStore = sessionStore)
            val offlineResolution = restartedGateway.resolve(spec.copy(endpointId = "endpoint-control-down"))
            val relayResolution = restartedGateway.resolve(spec.copy(endpointId = "endpoint-relay", relayMode = RelayMode.RELAY_ONLY))
            assertEquals("turn:127.0.0.1:41003?transport=udp", relayResolution.iceServers.single().urls.single())
            val answer = restartedGateway.createSignalingSession(
                spec,
                offlineResolution,
                ManagedSignalOffer("offer-sdp"),
                ManagedEndpointContract.dialPolicy(RelayMode.DIRECT),
            )
            assertEquals("answer-sdp", answer.sdp)
            assertEquals(listOf("candidate:streamed", "candidate:answer"), answer.candidates)

            assertEquals(1, records.count { it.path == "/v1/login/begin" })
            assertEquals(1, records.count { it.path == "/v1/login/complete" })
            assertTrue(records.filter { it.path == "/v1/endpoints/resolve" }.all { it.authorization == bearer })
            assertEquals(bearer, records.single { it.path == "/v1/signaling/create" }.authorization)
            assertEquals(bearer, records.single { it.path == "/v1/relay/leases/acquire" }.authorization)
            assertEquals(2, records.count { it.path.startsWith("/v1/login/") })
            hub.assertHealthy()
        } finally {
            control.close()
            hub.close()
        }
    }

    @Test
    fun rejectsNonLoopbackOrProductionLikeDevOrigins() {
        val failure = assertThrows(ManagedEndpointFailure::class.java) {
            DevCloudMobileGateway("https://cloud.termx.example", "http://127.0.0.1:41002", now = { now })
        }
        assertEquals("protocol", failure.code)
    }

    @Test
    fun acceptsPublicHTTPOnlyWhenExplicitlyEnabled() {
        DevCloudMobileGateway(
            "http://114.66.58.243:41101",
            "http://114.66.58.243:41102",
            allowPublicHTTP = true,
            now = { now },
        )
        val failure = assertThrows(ManagedEndpointFailure::class.java) {
            DevCloudMobileGateway("http://114.66.58.243:41101", "http://114.66.58.243:41102", now = { now })
        }
        assertEquals("protocol", failure.code)
    }

    @Test
    fun cachedSessionRejectsHubChangeAndDirectoryRollback() {
        val store = MemoryCloudSessionStore()
        val current = AccountSession(ByteArray(32) { 1 }, now.plusSeconds(300), "account-1", "Account One", "client-1", "hub-1", "https://hub.example.test", "region-1", 2)
        store.save(current)

        val rollback = assertThrows(ManagedEndpointFailure::class.java) {
            store.save(current.copy(directoryVersion = 1))
        }
        assertEquals("unauthenticated", rollback.code)
        val hubChange = assertThrows(ManagedEndpointFailure::class.java) {
            store.save(current.copy(hubId = "hub-2", directoryVersion = 3))
        }
        assertEquals("unauthenticated", hubChange.code)
    }

    private data class RecordedRequest(
        val path: String,
        val authorization: String,
        val contentType: String,
        val body: ByteArray,
    )

    private data class TestResponse(val contentType: String, val body: ByteArray)

    private class TestHttpServer(private val handler: (RecordedRequest) -> TestResponse) : AutoCloseable {
        private val server = ServerSocket(0, 16, InetAddress.getByName("127.0.0.1"))
        private val failure = AtomicReference<Throwable?>(null)
        private val worker = thread(name = "termx-mobile-cloud-test-${server.localPort}", isDaemon = true) {
            while (!server.isClosed) {
                try {
                    server.accept().use(::serve)
                } catch (closed: SocketException) {
                    if (!server.isClosed) failure.compareAndSet(null, closed)
                } catch (problem: Throwable) {
                    failure.compareAndSet(null, problem)
                }
            }
        }

        val origin: String get() = "http://127.0.0.1:${server.localPort}"

        fun assertHealthy() {
            failure.get()?.let { throw AssertionError("loopback HTTP harness failed", it) }
        }

        override fun close() {
            server.close()
            worker.join(2_000)
        }

        private fun serve(socket: Socket) {
            socket.soTimeout = 5_000
            val input = BufferedInputStream(socket.getInputStream())
            val requestLine = readLine(input)
            val requestParts = requestLine.split(' ')
            if (requestParts.size != 3 || requestParts[0] != "POST") throw AssertionError("invalid HTTP request line $requestLine")
            val headers = linkedMapOf<String, String>()
            while (true) {
                val line = readLine(input)
                if (line.isEmpty()) break
                val separator = line.indexOf(':')
                if (separator <= 0) throw AssertionError("invalid HTTP header")
                headers[line.substring(0, separator).trim().lowercase()] = line.substring(separator + 1).trim()
            }
            val length = headers["content-length"]?.toIntOrNull() ?: 0
            if (length !in 0..MAX_TEST_BODY_BYTES) throw AssertionError("invalid HTTP body length")
            val body = ByteArray(length)
            var offset = 0
            while (offset < body.size) {
                val count = input.read(body, offset, body.size - offset)
                if (count < 0) throw AssertionError("HTTP body ended early")
                offset += count
            }
            val response = try {
                handler(
                    RecordedRequest(
                        path = requestParts[1].substringBefore('?'),
                        authorization = headers["authorization"].orEmpty(),
                        contentType = headers["content-type"].orEmpty(),
                        body = body,
                    ),
                )
            } catch (problem: Throwable) {
                failure.compareAndSet(null, problem)
                TestResponse("application/x-protobuf", CloudCompanion.CloudError.newBuilder()
                    .setCode(CloudCompanion.CloudErrorCode.CLOUD_ERROR_CODE_PROTOCOL)
                    .setMessage("test harness rejected request")
                    .build()
                    .toByteArray())
            }
            val output = BufferedOutputStream(socket.getOutputStream())
            output.write("HTTP/1.1 200 OK\r\n".toByteArray(StandardCharsets.US_ASCII))
            output.write("Content-Type: ${response.contentType}\r\n".toByteArray(StandardCharsets.US_ASCII))
            output.write("Content-Length: ${response.body.size}\r\n".toByteArray(StandardCharsets.US_ASCII))
            output.write("Connection: close\r\n\r\n".toByteArray(StandardCharsets.US_ASCII))
            output.write(response.body)
            output.flush()
        }

        private fun readLine(input: BufferedInputStream): String {
            val output = ByteArrayOutputStream()
            while (output.size() <= MAX_TEST_LINE_BYTES) {
                val value = input.read()
                if (value < 0) throw AssertionError("HTTP headers ended early")
                if (value == '\n'.code) break
                if (value != '\r'.code) output.write(value)
            }
            if (output.size() > MAX_TEST_LINE_BYTES) throw AssertionError("HTTP line is too long")
            return output.toString(StandardCharsets.US_ASCII.name())
        }

        private companion object {
            const val MAX_TEST_BODY_BYTES = 4 shl 20
            const val MAX_TEST_LINE_BYTES = 16 * 1024
        }
    }

    private fun protoResponse(payload: ByteArray): TestResponse = TestResponse("application/x-protobuf", payload)

    private fun jsonResponse(payload: JSONObject): TestResponse = TestResponse(
        "application/json",
        payload.toString().toByteArray(Charsets.UTF_8),
    )

    private fun streamResponse(vararg events: CloudCompanion.SignalingEvent): TestResponse {
        val output = ByteArrayOutputStream()
        DataOutputStream(output).use { stream ->
            events.forEach { event ->
                val payload = event.toByteArray()
                stream.writeInt(payload.size)
                stream.write(payload)
            }
        }
        return TestResponse("application/x-termx-cloud-stream", output.toByteArray())
    }
}
