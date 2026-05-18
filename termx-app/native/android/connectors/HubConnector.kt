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
 * HubConnector — 通过托管 Hub 建立 WebRTC 连接（hub 路径）
 *
 * 流程：
 *  1. POST /api/v1/sessions/ice    → ICE 服务器列表
 *  2. WebRTCTransport.connectHub() → SDP offer + poll answer
 */
object HubConnector {

    private const val TAG = "TermxHubConnector"

    sealed class Result {
        data class Success(val transport: WebRTCTransport, val hubUrl: String, val relayInUse: Boolean) : Result()
        data class Failure(val reason: String) : Result()
    }

    suspend fun connect(
        machineId: String,
        hubUrls: List<String>,
        sessionToken: String,
        bridge: BridgeServer?,
        forceRelay: Boolean = false,
        onProgress: ((String) -> Unit)? = null,
    ): Result {
        for (hubUrl in hubUrls) {
            val result = tryHub(machineId, hubUrl, sessionToken, bridge, forceRelay, onProgress)
            coroutineContext.ensureActive()
            if (result is Result.Success) return result
            if (result is Result.Failure && result.reason == "auth") return result
        }
        return Result.Failure("all_hubs_failed")
    }

    private suspend fun tryHub(
        machineId: String,
        hubUrl: String,
        sessionToken: String,
        bridge: BridgeServer?,
        forceRelay: Boolean,
        onProgress: ((String) -> Unit)?,
    ): Result {
        var transport: WebRTCTransport? = null
        return try {
            onProgress?.invoke("Fetching ICE servers...")
            val iceServers = runInterruptible(Dispatchers.IO) {
                fetchIceServers(hubUrl, machineId, sessionToken)
            }
            coroutineContext.ensureActive()

            onProgress?.invoke("Connecting via hub...")
            val t = WebRTCTransport(bridge, machineId)
            transport = t
            val ok = runInterruptible(Dispatchers.IO) {
                t.connectHub(hubUrl, sessionToken, iceServers, forceRelay, onProgress)
            }
            coroutineContext.ensureActive()
            if (ok) {
                transport = null
                Result.Success(t, hubUrl, t.currentRelayInUse())
            } else {
                t.disconnect()
                Result.Failure(t.lastFailureReason ?: "connect_failed")
            }
        } catch (e: CancellationException) {
            transport?.disconnect()
            throw e
        } catch (e: Exception) {
            transport?.disconnect()
            Log.e(TAG, "hub $hubUrl connect failed", e)
            Result.Failure("exception")
        }
    }

    private fun fetchIceServers(hubUrl: String, machineId: String, sessionToken: String): JSONArray? {
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
}
