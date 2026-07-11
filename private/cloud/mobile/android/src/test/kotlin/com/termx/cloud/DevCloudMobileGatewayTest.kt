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
            assertEquals("/v1/signaling/create", request.path)
            val envelope = JSONObject(String(request.body, Charsets.UTF_8))
            val signalingRequest = CloudCompanion.CreateSignalingSessionRequest.parseFrom(
                Base64.getDecoder().decode(envelope.getString("payload")),
            )
            assertEquals("offer-sdp", signalingRequest.offerSdp)
            assertFalse(envelope.toString().contains("capability", ignoreCase = true))
            streamResponse(
                CloudCompanion.SignalingEvent.newBuilder()
                    .setCandidate(CloudCompanion.IceCandidate.newBuilder().setCandidate("candidate:streamed"))
                    .build(),
                CloudCompanion.SignalingEvent.newBuilder()
                    .setAnswer(
                        CloudCompanion.SignalingAnswer.newBuilder()
                            .setSignalingSessionId("signal-1")
                            .setSdp("answer-sdp")
                            .addCandidates(CloudCompanion.IceCandidate.newBuilder().setCandidate("candidate:answer")),
                    )
                    .build(),
            )
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
                        .put("access_token", Base64.getEncoder().encodeToString(accessToken)))
                }
                "/v1/endpoints/resolve" -> {
                    val parsed = CloudCompanion.ResolveEndpointRequest.parseFrom(request.body)
                    protoResponse(
                        CloudCompanion.ResolvedEndpoint.newBuilder()
                            .setEndpointId(parsed.endpointId)
                            .setTargetDeviceId(parsed.targetDeviceId)
                            .setPresence(CloudCompanion.PresenceState.PRESENCE_STATE_ONLINE)
                            .setHubId("hub-dev-1")
                            .setHubUrl(hub.origin)
                            .setManagedSessionId("managed-session-1")
                            .build()
                            .toByteArray(),
                    )
                }
                "/v1/admissions/client" -> {
                    val parsed = CloudCompanion.CreateSignalingSessionRequest.parseFrom(request.body)
                    jsonResponse(JSONObject()
                        .put("reference", "admission-1")
                        .put("hub_id", "hub-dev-1")
                        .put("account_id", "account-1")
                        .put("device_id", "android-client-1")
                        .put("target_device_id", parsed.targetDeviceId)
                        .put("session_kind", "managed")
                        .put("session_id", parsed.managedSessionId)
                        .put("expires_at_unix", now.plusSeconds(60).epochSecond)
                        .put("ticket", Base64.getEncoder().encodeToString(ByteArray(32) { 0x42 })))
                }
                else -> throw AssertionError("unexpected Control Plane path ${request.path}")
            }
        }

        try {
            val gateway = DevCloudMobileGateway(control.origin, hub.origin, now = { now })
            val spec = ManagedEndpointSpec(
                endpointId = "endpoint-1",
                targetDeviceId = "daemon-1",
                deviceFingerprint = "ed25519-sha256:fixture",
                grantRef = "grant-ref-1",
                relayMode = RelayMode.DIRECT,
            )
            val resolution = gateway.resolve(spec)
            assertEquals("managed-session-1", resolution.managedSessionId)
            val answer = gateway.createSignalingSession(
                spec,
                resolution,
                ManagedSignalOffer("offer-sdp"),
                ManagedEndpointContract.dialPolicy(RelayMode.DIRECT),
            )
            assertEquals("answer-sdp", answer.sdp)
            assertEquals(listOf("candidate:streamed", "candidate:answer"), answer.candidates)

            assertEquals(1, records.count { it.path == "/v1/login/begin" })
            assertEquals(1, records.count { it.path == "/v1/login/complete" })
            assertEquals(bearer, records.single { it.path == "/v1/endpoints/resolve" }.authorization)
            assertEquals(bearer, records.single { it.path == "/v1/admissions/client" }.authorization)
            assertTrue(records.single { it.path == "/v1/signaling/create" }.authorization.isEmpty())
            control.assertHealthy()
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
