package com.termx.app.connectors

import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.transport.WebRTCTransport
import kotlinx.coroutines.*

/**
 * RaceConnector — 并发启动 LocalConnector 和 HubConnector，取先成功者
 */
object RaceConnector {

    private const val TAG = "TermxRaceConnector"

    sealed class Result {
        data class Success(val transport: WebRTCTransport, val path: String, val relayInUse: Boolean) : Result()
        data class Failure(val reason: String) : Result()
    }

    suspend fun race(
        machineId: String,
        localAddresses: List<String>,
        hubUrls: List<String>,
        sessionToken: String,
        bridge: BridgeServer?,
        preferredPath: String = "local",
        forceRelay: Boolean = false,
        onProgress: ((String) -> Unit)? = null,
    ): Result = coroutineScope {
        val winner = CompletableDeferred<Result>()

        val localJob = async(Dispatchers.IO) {
            if (forceRelay) return@async null
            if (localAddresses.isEmpty()) return@async null
            try {
                val r = LocalConnector.connect(machineId, localAddresses, sessionToken, bridge, onProgress)
                if (r is LocalConnector.Result.Success) {
                    winner.complete(Result.Success(r.transport, "local", false))
                }
                r
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { Log.e(TAG, "local failed", e); null }
        }

        val hubJob = async(Dispatchers.IO) {
            if (hubUrls.isEmpty()) return@async null
            // For preferred local, delay hub start slightly
            if (!forceRelay && preferredPath == "local" && localAddresses.isNotEmpty()) {
                delay(500)
            }
            try {
                val r = HubConnector.connect(machineId, hubUrls, sessionToken, bridge, forceRelay, onProgress)
                if (r is HubConnector.Result.Success) {
                    winner.complete(Result.Success(r.transport, "hub", r.relayInUse))
                }
                r
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { Log.e(TAG, "hub failed", e); null }
        }

        launch {
            localJob.join(); hubJob.join()
            winner.complete(Result.Failure("all_failed"))
        }

        val result = winner.await()

        if (result is Result.Success) {
            when (result.path) {
                "local" -> {
                    hubJob.cancel()
                    cleanupLoser(hubJob)
                }
                else -> {
                    localJob.cancel()
                    cleanupLoser(localJob)
                }
            }
        }

        result
    }

    private suspend fun cleanupLoser(job: Deferred<Any?>) {
        try {
            val r = if (job.isCompleted && !job.isCancelled) job.await() else return
            when (r) {
                is LocalConnector.Result.Success -> r.transport.disconnect()
                is HubConnector.Result.Success -> r.transport.disconnect()
            }
        } catch (_: Exception) {}
    }
}
