package com.termx.app.transport

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.webrtc.RTCStats

/** RemoteCertificateFingerprintTest 证明 endpoint trust pin 只来自 selected transport 引用的远端证书。 */
class RemoteCertificateFingerprintTest {
    @Test
    fun readsOnlyTransportBoundSha256Certificate() {
        val rawFingerprint = List(32) { "AB" }.joinToString(":")
        val stats = mapOf(
            "transport-1" to RTCStats(1, "transport", "transport-1", mapOf("remoteCertificateId" to "certificate-1")),
            "certificate-1" to RTCStats(1, "certificate", "certificate-1", mapOf(
                "fingerprintAlgorithm" to "sha-256",
                "fingerprint" to rawFingerprint,
            )),
            "unbound-certificate" to RTCStats(1, "certificate", "unbound-certificate", mapOf(
                "fingerprintAlgorithm" to "sha-256",
                "fingerprint" to List(32) { "22" }.joinToString(":"),
            )),
        )

        assertEquals("sha-256:" + List(32) { "ab" }.joinToString(":"), remoteCertificateFingerprintFromStats(stats))
    }

    @Test
    fun rejectsMissingBindingAndNonSha256Certificate() {
        assertNull(remoteCertificateFingerprintFromStats(mapOf(
            "certificate-1" to RTCStats(1, "certificate", "certificate-1", mapOf(
                "fingerprintAlgorithm" to "sha-256",
                "fingerprint" to List(32) { "11" }.joinToString(":"),
            )),
        )))
        assertNull(remoteCertificateFingerprintFromStats(mapOf(
            "transport-1" to RTCStats(1, "transport", "transport-1", mapOf("remoteCertificateId" to "certificate-1")),
            "certificate-1" to RTCStats(1, "certificate", "certificate-1", mapOf(
                "fingerprintAlgorithm" to "sha-1",
                "fingerprint" to List(20) { "11" }.joinToString(":"),
            )),
        )))
    }
}
