package com.anytty.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class NativeConnectionRuntimeCoordinatorTest {
    @Test
    fun delayedNetworkAvailabilityCannotRestartAfterReset() {
        val harness = Harness()
        harness.coordinator.load("wifi")
        harness.operations.clear()

        harness.coordinator.onNetworkAvailable("cellular")
        assertEquals(listOf("changing:network_available:1"), harness.events)
        assertEquals(1, harness.pending.size)

        harness.coordinator.resetLocalPairings()
        assertEquals(listOf("reset"), harness.operations)
        assertTrue(harness.pending.single().cancelled)

        harness.pending.single().runEvenIfCancelled()
        assertEquals(listOf("reset"), harness.operations)
        assertFalse(harness.events.any { it.startsWith("changed:") })
    }

    @Test
    fun destroyCancelsDelayedRestartAndLaterStartsFailClosed() {
        val harness = Harness()
        harness.coordinator.load("wifi")
        harness.operations.clear()

        harness.coordinator.onNetworkAvailable("cellular")
        val delayed = harness.pending.single()
        harness.coordinator.destroy()

        assertTrue(delayed.cancelled)
        assertEquals(listOf("suspend"), harness.operations)
        delayed.runEvenIfCancelled()
        harness.coordinator.onNetworkAvailable("wifi")

        assertEquals(listOf("suspend"), harness.operations)
        assertThrows(IllegalStateException::class.java) { harness.coordinator.restartForForeground() }
        assertThrows(IllegalStateException::class.java) { harness.coordinator.resetLocalPairings() }
        assertEquals(1, harness.pending.size)
    }

    private class Harness {
        val operations = mutableListOf<String>()
        val events = mutableListOf<String>()
        val pending = mutableListOf<ManualPendingWork>()
        val coordinator = NativeConnectionRuntimeCoordinator(
            scheduleNetworkRestart = { _, task -> ManualPendingWork(task).also(pending::add) },
            isApplicationStarted = { true },
            startRuntime = { operations += "start" },
            restartRuntime = { operations += "restart" },
            suspendRuntime = { operations += "suspend" },
            resetRuntime = { operations += "reset" },
            generationChanging = { reason, epoch -> events += "changing:$reason:$epoch" },
            generationChanged = { reason, epoch -> events += "changed:$reason:$epoch" },
            generationChangeFailed = { reason, epoch, _ -> events += "failed:$reason:$epoch" },
        )
    }

    private class ManualPendingWork(private val task: () -> Unit) : PendingRuntimeWork {
        var cancelled = false
            private set

        override fun cancel() {
            cancelled = true
        }

        fun runEvenIfCancelled() {
            task()
        }
    }
}
