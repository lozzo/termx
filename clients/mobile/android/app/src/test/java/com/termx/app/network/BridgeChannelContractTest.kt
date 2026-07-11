package com.termx.app.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** BridgeChannelContractTest 固定 Android WebView/native bridge 只开放一个 endpoint-scoped protocol 标签。 */
class BridgeChannelContractTest {
    @Test
    fun acceptsOnlyCanonicalProtocolLabel() {
        assertEquals("daemon-1", protocolBridgeEndpoint("protocol:daemon-1"))
        assertNull(protocolBridgeEndpoint("api:daemon-1"))
        assertNull(protocolBridgeEndpoint("events:daemon-1"))
        assertNull(protocolBridgeEndpoint("terminal:daemon-1:terminal-1"))
        assertNull(protocolBridgeEndpoint("file:daemon-1:transfer-1"))
        assertNull(protocolBridgeEndpoint("protocol:"))
        assertNull(protocolBridgeEndpoint(" protocol:daemon-1"))
        assertNull(protocolBridgeEndpoint("protocol:daemon 1"))
    }
}
