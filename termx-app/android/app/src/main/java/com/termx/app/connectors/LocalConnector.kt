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
    private const val PROBE_TIMEOUT = 3000
    private const val STATUS_PATH = "/api/local/status"

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
            val hubUrl = probeLocalHub(localAddresses)
            coroutineContext.ensureActive()
            if (hubUrl == null) return Result.Failure("unreachable")

            Log.i(TAG, "Found local hub: $hubUrl")
            onProgress?.invoke("Connecting locally...")

            val iceServers = fetchIceServers(hubUrl, machineId, sessionToken)
            coroutineContext.ensureActive()

            val t = WebRTCTransport(bridge, machineId)
            transport = t
            val ok = withContext(Dispatchers.IO) {
                t.connectHub(hubUrl, sessionToken, iceServers, onProgress = onProgress)
            }
            coroutineContext.ensureActive()
            if (ok) { transport = null; return Result.Success(t) }
            t.disconnect()
        } catch (e: CancellationException) {
            transport?.disconnect()
            throw e
        } catch (e: Exception) {
            transport?.disconnect()
            Log.e(TAG, "local connect failed", e)
        }
        return Result.Failure("connect_failed")
    }

    private suspend fun probeLocalHub(addresses: List<String>): String? {
        if (addresses.isEmpty()) return null

        // Try each address concurrently, return first success
        return coroutineScope {
            val winner = CompletableDeferred<String?>()
            val jobs = addresses.map { addr ->
                async(Dispatchers.IO) {
                    val url = normalizeUrl(addr)
                    val status = HttpHelper.probe("$url$STATUS_PATH", PROBE_TIMEOUT)
                    if (status > 0 && status < 500) {
                        winner.complete(url)
                    }
                }
            }
            launch {
                jobs.forEach { it.join() }
                winner.complete(null) // all failed
            }
            val result = winner.await()
            jobs.forEach { it.cancel() }
            result
        }
    }

    private fun fetchIceServers(hubUrl: String, machineId: String, sessionToken: String): org.json.JSONArray? {
        return try {
            val headers = mapOf(
                "Content-Type" to "application/json",
                "Authorization" to "Bearer $sessionToken",
            )
            val body = JSONObject()
                .put("machine_id", machineId)
                .put("session_token", sessionToken)
                .toString()
            val resp = HttpHelper.post("$hubUrl/api/v1/sessions/ice", headers, body, 8000)
            if (!resp.isOk) return null
            JSONObject(resp.bodyString()).optJSONArray("ice_servers")
        } catch (e: Exception) {
            Log.w(TAG, "fetchIceServers failed: ${e.message}")
            null
        }
    }

    private fun normalizeUrl(addr: String): String = addr.trim().trimEnd('/')
}
