package com.termx.app.connectors

import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.transport.WebRTCTransport
import com.termx.app.util.HttpHelper
import kotlinx.coroutines.*
import org.json.JSONArray
import org.json.JSONObject
import kotlin.coroutines.coroutineContext

/**
 * LocalConnector — 探测局域网地址，通过本地 Hub 建立 WebRTC 连接
 *
 * 流程：
 *  1. 并发探测 localAddresses，找到可达的本地 Hub URL
 *  2. POST /api/v1/sessions/ice → 获取 ICE 服务器（或空列表）
 *  3. WebRTCTransport.connectHub(...) 完成信令
 */
object LocalConnector {

    private const val TAG = "TermxLocalConnector"
    private const val ICE_PATH = "/api/v1/sessions/ice"
    private const val ICE_PROBE_TIMEOUT = 8000

    private data class LocalHubProbe(val hubUrl: String, val iceServers: JSONArray?)
    private data class IceServerProbe(val ok: Boolean, val iceServers: JSONArray?)

    sealed class Result {
        data class Success(val transport: WebRTCTransport) : Result()
        data class Failure(val reason: String) : Result()
    }

    suspend fun connect(
        machineId: String,
        localAddresses: List<String>,
        sessionToken: String,
        bridge: BridgeServer?,
        onProgress: ((String) -> Unit)? = null,
    ): Result {
        var transport: WebRTCTransport? = null
        try {
            onProgress?.invoke("Probing local addresses...")
            val localHub = probeLocalHub(localAddresses, machineId, sessionToken)
            coroutineContext.ensureActive()
            if (localHub == null) return Result.Failure("unreachable")

            Log.i(TAG, "Found local hub: ${localHub.hubUrl}")
            onProgress?.invoke("Connecting locally...")

            val t = WebRTCTransport(bridge, machineId)
            transport = t
            val ok = runInterruptible(Dispatchers.IO) {
                t.connectHub(localHub.hubUrl, sessionToken, localHub.iceServers, onProgress = onProgress)
            }
            coroutineContext.ensureActive()
            if (ok) { transport = null; return Result.Success(t) }
            val reason = t.lastFailureReason ?: "connect_failed"
            t.disconnect()
            return Result.Failure(reason)
        } catch (e: CancellationException) {
            transport?.disconnect()
            throw e
        } catch (e: Exception) {
            transport?.disconnect()
            Log.e(TAG, "local connect failed", e)
        }
        return Result.Failure("connect_failed")
    }

    private suspend fun probeLocalHub(addresses: List<String>, machineId: String, sessionToken: String): LocalHubProbe? {
        if (addresses.isEmpty()) return null

        // 本地探测也必须走 Hub/core-v2 session API，不能访问 App 私有 local status。
        return coroutineScope {
            val winner = CompletableDeferred<LocalHubProbe?>()
            val jobs = addresses.map { addr ->
                async(Dispatchers.IO) {
                    val url = normalizeUrl(addr)
                    val probe = runInterruptible {
                        requestIceServers(url, machineId, sessionToken)
                    }
                    if (probe.ok) {
                        winner.complete(LocalHubProbe(url, probe.iceServers))
                    }
                }
            }
            launch {
                jobs.forEach { it.join() }
                winner.complete(null)
            }
            val result = winner.await()
            jobs.forEach { it.cancel() }
            result
        }
    }

    private fun requestIceServers(hubUrl: String, machineId: String, sessionToken: String): IceServerProbe {
        return try {
            val headers = mapOf(
                "Content-Type" to "application/json",
                "Authorization" to "Bearer $sessionToken",
            )
            val body = JSONObject()
                .put("machine_id", machineId)
                .put("session_token", sessionToken)
                .toString()
            val resp = HttpHelper.post("$hubUrl$ICE_PATH", headers, body, ICE_PROBE_TIMEOUT)
            if (!resp.isOk) return IceServerProbe(false, null)
            val responseBody = resp.bodyString()
            val iceServers = if (responseBody.isBlank()) null else JSONObject(responseBody).optJSONArray("ice_servers")
            IceServerProbe(true, iceServers)
        } catch (e: Exception) {
            Log.w(TAG, "probe local hub failed: ${e.message}")
            IceServerProbe(false, null)
        }
    }

    private fun normalizeUrl(addr: String): String = addr.trim().trimEnd('/')
}
