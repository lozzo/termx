package com.termx.app.endpoint

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.nio.charset.StandardCharsets
import java.util.TreeMap

/**
 * EndpointRegistryCodec 严格读写 Android native registry。
 * 未知字段、旧版本、超限输入、身份冲突和 kind-specific 字段混用都 fail closed；secret 与 runtime 状态没有编码字段。
 */
object EndpointRegistryCodec {
    /** decode 从普通本地文件读取同桌面 v2 schema 等价的 JSON 表达。 */
    fun decode(payload: ByteArray): SavedEndpointRegistry {
        if (payload.size > ENDPOINT_REGISTRY_MAX_BYTES) fail("size_limit", "endpoint registry exceeds size limit")
        if (payload.toString(StandardCharsets.UTF_8).isBlank()) fail("config_invalid", "endpoint registry is empty")
        try {
            return parseRegistry(JSONObject(payload.toString(StandardCharsets.UTF_8)))
        } catch (failure: EndpointContractException) {
            throw failure
        } catch (failure: JSONException) {
            fail("config_invalid", "endpoint registry JSON is invalid")
        }
    }

    /** encode 在校验后输出稳定字段顺序的 JSON；源 credential body、Cloud token、grant 和 runtime 不可进入输出。 */
    fun encode(registry: SavedEndpointRegistry): ByteArray {
        validateRegistry(registry)
        val root = JSONObject()
            .put("version", ENDPOINT_REGISTRY_VERSION)
            .put("default", registry.defaultEndpointId)
        val endpoints = JSONObject()
        TreeMap(registry.endpoints).forEach { (endpointId, endpoint) -> endpoints.put(endpointId, encodeEndpoint(endpoint)) }
        root.put("endpoints", endpoints)
        val payload = root.toString(2).toByteArray(StandardCharsets.UTF_8)
        if (payload.size > ENDPOINT_REGISTRY_MAX_BYTES) fail("size_limit", "encoded endpoint registry exceeds size limit")
        return payload
    }

    internal fun parseRegistry(root: JSONObject): SavedEndpointRegistry {
        requireOnly(root, setOf("version", "default", "endpoints"))
        val version = root.requireInt("version")
        if (version != ENDPOINT_REGISTRY_VERSION) fail("unsupported_version", "unsupported endpoint registry version")
        val defaultEndpointId = root.requireString("default")
        val endpointObject = root.requireObject("endpoints")
        val endpoints = linkedMapOf<String, SavedEndpoint>()
        for (endpointId in endpointObject.keyList().sorted()) {
            validateIdentifier("endpoint", endpointId)
            endpoints[endpointId] = parseEndpoint(endpointId, endpointObject.requireObject(endpointId))
        }
        return SavedEndpointRegistry(version, defaultEndpointId, endpoints).also(::validateRegistry)
    }

    internal fun parseCandidate(value: JSONObject): AndroidEndpointCandidate {
        requireOnly(
            value,
            setOf(
                "source", "identity", "suggested_label", "routes", "connect_mode", "selection_policy",
                "apply_client_policy", "credential_descriptors",
            ),
        )
        val source = EndpointSource.parse(value.requireString("source"))
        if (source == EndpointSource.LAN) fail("config_invalid", "LAN candidate cannot enter persistent assembler")
        val identityObject = value.requireObject("identity")
        requireOnly(identityObject, setOf("device_id", "device_fingerprint"))
        val identity = EndpointDaemonIdentity(identityObject.requireString("device_id"), identityObject.requireString("device_fingerprint"))
        identity.validate(true)
        val routes = mutableListOf<SavedAccessRoute>()
        val routeArray = value.optionalArray("routes") ?: JSONArray()
        for (index in 0 until routeArray.length()) {
            routes += parseRoute(routeArray.getJSONObject(index), null, source)
        }
        val credentialDescriptors = mutableListOf<AndroidCredentialDescriptor>()
        val credentialArray = value.optionalArray("credential_descriptors") ?: JSONArray()
        for (index in 0 until credentialArray.length()) {
            credentialDescriptors += parseCredentialDescriptor(credentialArray.getJSONObject(index))
        }
        val selection = value.optionalObject("selection_policy")?.let(::parseSelectionPolicy)
        return AndroidEndpointCandidate(
            source = source,
            identity = identity,
            suggestedLabel = value.optionalString("suggested_label"),
            routes = routes,
            connectMode = value.optionalString("connect_mode").takeIf(String::isNotEmpty)?.let(EndpointConnectMode::parse),
            selectionPolicy = selection,
            applyClientPolicy = value.optionalBoolean("apply_client_policy", false),
            credentialDescriptors = credentialDescriptors,
        )
    }

    private fun parseCredentialDescriptor(value: JSONObject): AndroidCredentialDescriptor {
        requireOnly(value, setOf("descriptor_id", "kind", "exportable"))
        return AndroidCredentialDescriptor(
            descriptorId = value.requireString("descriptor_id"),
            kind = EndpointCredentialKind.parse(value.requireString("kind")),
            exportable = value.optionalBoolean("exportable", false),
        ).also(::validateCredentialDescriptor)
    }

    private fun parseEndpoint(endpointId: String, value: JSONObject): SavedEndpoint {
        requireOnly(value, setOf("label", "label_source", "device_id", "device_fingerprint", "enabled", "connect_mode", "selection", "routes"))
        val identity = EndpointDaemonIdentity(value.optionalString("device_id"), value.optionalString("device_fingerprint"))
        val routesObject = value.requireObject("routes")
        val routes = linkedMapOf<String, SavedAccessRoute>()
        for (routeId in routesObject.keyList().sorted()) {
            validateIdentifier("route", routeId)
            routes[routeId] = parseRoute(routesObject.requireObject(routeId), routeId, null)
        }
        return SavedEndpoint(
            endpointId = endpointId,
            label = value.optionalString("label", endpointId).ifBlank { endpointId },
            labelSource = EndpointSource.parse(value.optionalString("label_source", "manual")),
            daemonIdentity = identity,
            connectMode = EndpointConnectMode.parse(value.optionalString("connect_mode", "on_demand")),
            enabled = value.optionalBoolean("enabled", true),
            selectionPolicy = value.optionalObject("selection")?.let(::parseSelectionPolicy) ?: EndpointSelectionPolicy(),
            routes = routes,
        )
    }

    private fun parseSelectionPolicy(value: JSONObject): EndpointSelectionPolicy {
        requireOnly(value, setOf("hedge_delay", "hedge_delay_millis", "hedge_delay_configured"))
        if (value.has("hedge_delay")) {
            return EndpointSelectionPolicy(parseDurationMillis(value.requireString("hedge_delay")), true)
        }
        val configured = value.optionalBoolean("hedge_delay_configured", value.has("hedge_delay_millis"))
        return EndpointSelectionPolicy(value.optionalLong("hedge_delay_millis") ?: 0, configured)
    }

    private fun parseRoute(value: JSONObject, mapRouteId: String?, candidateSource: EndpointSource?): SavedAccessRoute {
        requireOnly(value, ROUTE_FIELDS)
        val routeId = mapRouteId ?: value.requireIdentifier("route_id", "route")
        val source = if (value.has("source")) EndpointSource.parse(value.requireString("source")) else candidateSource ?: EndpointSource.MANUAL
        val policySource = if (value.has("policy_source")) EndpointSource.parse(value.requireString("policy_source")) else source
        val hostKeys = value.optionalArray("host_key_fingerprints").stringList()
        val addresses = value.optionalArray("addresses").stringList()
        val kind = EndpointRouteKind.parse(value.requireString("kind"))
        val relayMode = value.optionalString("relay_mode").takeIf(String::isNotEmpty)?.let(EndpointRelayMode::parse)
        val remoteSocket = value.optionalString("remote_socket").let { configured ->
            if (configured.isEmpty() && kind == EndpointRouteKind.SSH_STDIO) "auto" else configured
        }
        return SavedAccessRoute(
            routeId = routeId,
            kind = kind,
            enabled = value.optionalBoolean("enabled", true),
            manualOnly = value.optionalBoolean("manual_only", false),
            priority = value.optionalInt("priority"),
            credentialRef = value.optionalString("credential_ref"),
            source = source,
            policySource = policySource,
            socket = value.optionalString("socket"),
            host = value.optionalString("host"),
            port = value.optionalInt("port") ?: if (kind == EndpointRouteKind.SSH_STDIO) 22 else 0,
            user = value.optionalString("user"),
            proxyJump = value.optionalString("proxy_jump"),
            remoteSocket = remoteSocket,
            hostKeyFingerprints = hostKeys.sorted(),
            addresses = addresses.sorted(),
            serverName = value.optionalString("server_name"),
            targetDeviceId = value.optionalString("target_device_id"),
            accountProfile = value.optionalString("account_profile"),
            relayMode = relayMode ?: if (kind == EndpointRouteKind.MANAGED_WEBRTC) EndpointRelayMode.AUTO else null,
        )
    }

    private fun encodeEndpoint(endpoint: SavedEndpoint): JSONObject {
        val value = JSONObject()
            .put("label", endpoint.label)
            .put("label_source", endpoint.labelSource.wireName)
        if (!endpoint.daemonIdentity.isEmpty()) {
            value.put("device_id", endpoint.daemonIdentity.deviceId)
            value.put("device_fingerprint", endpoint.daemonIdentity.deviceFingerprint)
        }
        value.put("enabled", endpoint.enabled)
            .put("connect_mode", endpoint.connectMode.wireName)
        if (endpoint.selectionPolicy.hedgeDelayConfigured) {
            value.put("selection", JSONObject().put("hedge_delay", "${endpoint.selectionPolicy.hedgeDelayMillis}ms"))
        }
        val routes = JSONObject()
        TreeMap(endpoint.routes).forEach { (routeId, route) -> routes.put(routeId, encodeRoute(route)) }
        return value.put("routes", routes)
    }

    private fun encodeRoute(route: SavedAccessRoute): JSONObject {
        val value = JSONObject()
            .put("kind", route.kind.wireName)
            .put("enabled", route.enabled)
            .put("manual_only", route.manualOnly)
            .put("source", route.source.wireName)
            .put("policy_source", route.policySource.wireName)
        route.priority?.let { value.put("priority", it) }
        putNonBlank(value, "credential_ref", route.credentialRef)
        putNonBlank(value, "socket", route.socket)
        putNonBlank(value, "host", route.host)
        if (route.port != 0) value.put("port", route.port)
        putNonBlank(value, "user", route.user)
        putNonBlank(value, "proxy_jump", route.proxyJump)
        putNonBlank(value, "remote_socket", route.remoteSocket)
        if (route.hostKeyFingerprints.isNotEmpty()) value.put("host_key_fingerprints", JSONArray(route.hostKeyFingerprints))
        if (route.addresses.isNotEmpty()) value.put("addresses", JSONArray(route.addresses))
        putNonBlank(value, "server_name", route.serverName)
        putNonBlank(value, "target_device_id", route.targetDeviceId)
        putNonBlank(value, "account_profile", route.accountProfile)
        route.relayMode?.let { value.put("relay_mode", it.wireName) }
        return value
    }

    private fun putNonBlank(target: JSONObject, key: String, value: String) {
        if (value.isNotBlank()) target.put(key, value)
    }

    private val ROUTE_FIELDS = setOf(
        "route_id", "kind", "enabled", "manual_only", "priority", "credential_ref", "source", "policy_source",
        "socket", "host", "port", "user", "proxy_jump", "remote_socket", "host_key_fingerprints",
        "addresses", "server_name", "target_device_id", "account_profile", "relay_mode",
    )
}

internal fun validateRegistry(registry: SavedEndpointRegistry) {
    if (registry.version != ENDPOINT_REGISTRY_VERSION) fail("unsupported_version", "unsupported endpoint registry version")
    if (registry.defaultEndpointId != registry.defaultEndpointId.trim()) fail("config_invalid", "default endpoint is not canonical")
    val deviceIds = mutableMapOf<String, SavedEndpoint>()
    val fingerprints = mutableMapOf<String, SavedEndpoint>()
    registry.endpoints.toSortedMap().forEach { (key, endpoint) ->
        validateIdentifier("endpoint", key)
        if (key != endpoint.endpointId) fail("config_invalid", "endpoint key does not match endpoint_id")
        endpoint.daemonIdentity.validate(false)
        if (!endpoint.daemonIdentity.isEmpty()) {
            deviceIds[endpoint.daemonIdentity.deviceId]?.let { existing ->
                if (existing.daemonIdentity.deviceFingerprint == endpoint.daemonIdentity.deviceFingerprint) {
                    fail("identity_conflict", "multiple endpoints repeat the same daemon identity")
                }
                fail("identity_conflict", "device_id is pinned to multiple fingerprints")
            }
            fingerprints[endpoint.daemonIdentity.deviceFingerprint]?.let {
                fail("identity_conflict", "fingerprint is pinned to multiple device_id values")
            }
            deviceIds[endpoint.daemonIdentity.deviceId] = endpoint
            fingerprints[endpoint.daemonIdentity.deviceFingerprint] = endpoint
        }
        if (endpoint.routes.isEmpty()) fail("config_invalid", "endpoint requires at least one route")
        if (!endpoint.selectionPolicy.hedgeDelayConfigured && endpoint.selectionPolicy.hedgeDelayMillis != 0L) {
            fail("config_invalid", "hedge delay must be zero when it is not configured")
        }
        if (endpoint.selectionPolicy.hedgeDelayConfigured && endpoint.selectionPolicy.hedgeDelayMillis !in 0..30_000) {
            fail("config_invalid", "hedge delay is outside the supported range")
        }
        var anyPriority = false
        var allPriority = true
        endpoint.routes.forEach { (routeKey, route) ->
            if (routeKey != route.routeId) fail("config_invalid", "route key does not match route_id")
            validateRoute(route, endpoint.daemonIdentity)
            if (route.enabled && !route.manualOnly) {
                anyPriority = anyPriority || route.priority != null
                allPriority = allPriority && route.priority != null
            }
        }
        if (anyPriority && !allPriority) fail("config_invalid", "every enabled automatic route must configure priority")
    }
    if (registry.defaultEndpointId.isNotEmpty()) {
        val defaultEndpoint = registry.endpoints[registry.defaultEndpointId]
            ?: fail("config_invalid", "default endpoint is missing")
        if (!defaultEndpoint.enabled) fail("config_invalid", "default endpoint is disabled")
    } else if (registry.endpoints.isNotEmpty()) {
        fail("config_invalid", "non-empty registry requires a default endpoint")
    }
}

private fun validateRoute(route: SavedAccessRoute, identity: EndpointDaemonIdentity) {
    validateIdentifier("route", route.routeId)
    if (route.priority != null && route.priority < 0) fail("config_invalid", "route priority must be non-negative")
    if (route.port !in 0..65535) fail("config_invalid", "route port is invalid")
    listOf(
        "credential_ref" to route.credentialRef,
        "socket" to route.socket,
        "host" to route.host,
        "user" to route.user,
        "proxy_jump" to route.proxyJump,
        "remote_socket" to route.remoteSocket,
        "server_name" to route.serverName,
        "target_device_id" to route.targetDeviceId,
        "account_profile" to route.accountProfile,
    ).forEach { (field, value) -> validateCanonicalRouteText(route.routeId, field, value, false) }
    validateCanonicalRouteList(route.routeId, "host_key_fingerprints", route.hostKeyFingerprints, false)
    validateCanonicalRouteList(route.routeId, "addresses", route.addresses, route.kind == EndpointRouteKind.DIRECT_TLS)
    val hasSSH = route.host.isNotBlank() || route.port != 0 || route.user.isNotBlank() || route.proxyJump.isNotBlank() || route.remoteSocket.isNotBlank() || route.hostKeyFingerprints.isNotEmpty()
    val hasDirect = route.addresses.isNotEmpty() || route.serverName.isNotBlank()
    val hasManaged = route.targetDeviceId.isNotBlank() || route.accountProfile.isNotBlank() || route.relayMode != null
    when (route.kind) {
        EndpointRouteKind.LOCAL_UNIX -> {
            if (route.socket.isBlank() || hasSSH || hasDirect || hasManaged || route.credentialRef.isNotBlank()) fail("config_invalid", "local-unix route fields are invalid")
        }
        EndpointRouteKind.SSH_STDIO -> {
            if (route.host.isBlank() || route.socket.isNotBlank() || hasDirect || hasManaged) fail("config_invalid", "ssh-stdio route fields are invalid")
        }
        EndpointRouteKind.DIRECT_TLS -> {
            identity.validate(true)
            if (route.addresses.isEmpty() || route.socket.isNotBlank() || hasSSH || hasManaged) fail("config_invalid", "direct-tls route fields are invalid")
        }
        EndpointRouteKind.MANAGED_WEBRTC -> {
            identity.validate(true)
            if (route.targetDeviceId.isBlank() || route.targetDeviceId != identity.deviceId || route.relayMode == null || route.socket.isNotBlank() || hasSSH || hasDirect) {
                fail("config_invalid", "managed-webrtc route fields are invalid")
            }
        }
    }
}

private fun validateCanonicalRouteText(routeId: String, field: String, value: String, required: Boolean) {
    if (value.isEmpty()) {
        if (required) fail("config_invalid", "route $routeId field $field is required")
        return
    }
    if (value != value.trim() || value.any(Character::isISOControl)) {
        fail("config_invalid", "route $routeId field $field is not canonical")
    }
}

private fun validateCanonicalRouteList(routeId: String, field: String, values: List<String>, required: Boolean) {
    if (required && values.isEmpty()) fail("config_invalid", "route $routeId field $field requires at least one value")
    val seen = mutableSetOf<String>()
    values.forEach { value ->
        validateCanonicalRouteText(routeId, field, value, true)
        if (!seen.add(value)) fail("config_invalid", "route $routeId field $field repeats a value")
    }
}

internal fun validateIdentifier(kind: String, value: String) {
    if (!Regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$").matches(value)) fail("config_invalid", "$kind identifier is invalid")
}

private fun requireOnly(value: JSONObject, allowed: Set<String>) {
    val unknown = value.keyList().filterNot(allowed::contains)
    if (unknown.isNotEmpty()) fail("config_invalid", "unknown field ${unknown.sorted().first()}")
}

private fun JSONObject.keyList(): List<String> {
    val values = mutableListOf<String>()
    val iterator = keys()
    while (iterator.hasNext()) values += iterator.next()
    return values
}

private fun JSONObject.requireObject(name: String): JSONObject = if (has(name) && !isNull(name)) getJSONObject(name) else fail("config_invalid", "$name is required")
private fun JSONObject.requireString(name: String): String = if (has(name)) optionalString(name) else fail("config_invalid", "$name is required")
private fun JSONObject.requireInt(name: String): Int = optionalInt(name) ?: fail("config_invalid", "$name is required")

private fun JSONObject.requireIdentifier(name: String, kind: String): String {
    if (!has(name)) fail("config_invalid", "$name is required")
    val value = get(name)
    if (value !is String) fail("config_invalid", "$name must be a string")
    validateIdentifier(kind, value)
    return value
}

private fun JSONObject.optionalString(name: String, fallback: String = ""): String {
    if (!has(name)) return fallback
    val value = get(name)
    if (value !is String) fail("config_invalid", "$name must be a string")
    return value
}

private fun JSONObject.optionalBoolean(name: String, fallback: Boolean): Boolean {
    if (!has(name)) return fallback
    val value = get(name)
    if (value !is Boolean) fail("config_invalid", "$name must be a boolean")
    return value
}

private fun JSONObject.optionalInt(name: String): Int? {
    if (!has(name)) return null
    val value = get(name)
    return when (value) {
        is Int -> value
        is Long -> value.takeIf { it >= Int.MIN_VALUE.toLong() && it <= Int.MAX_VALUE.toLong() }?.toInt()
        else -> null
    } ?: fail("config_invalid", "$name must be an integer")
}

private fun JSONObject.optionalLong(name: String): Long? {
    if (!has(name)) return null
    val value = get(name)
    return when (value) {
        is Int -> value.toLong()
        is Long -> value
        else -> null
    } ?: fail("config_invalid", "$name must be an integer")
}

private fun JSONObject.optionalObject(name: String): JSONObject? {
    if (!has(name)) return null
    return get(name) as? JSONObject ?: fail("config_invalid", "$name must be an object")
}

private fun JSONObject.optionalArray(name: String): JSONArray? {
    if (!has(name)) return null
    return get(name) as? JSONArray ?: fail("config_invalid", "$name must be an array")
}

private fun JSONArray?.stringList(): List<String> {
    if (this == null) return emptyList()
    val values = mutableListOf<String>()
    for (index in 0 until length()) {
        val value = get(index)
        if (value !is String) fail("config_invalid", "string list contains a non-string value")
        values += value
    }
    if (values.any { it.isEmpty() || it != it.trim() || it.any(Character::isISOControl) }) {
        fail("config_invalid", "string list contains a non-canonical value")
    }
    if (values.toSet().size != values.size) fail("config_invalid", "string list contains a duplicate value")
    return values
}

private fun parseDurationMillis(value: String): Long {
    val match = Regex("^(\\d+)(ms|s)$").matchEntire(value) ?: fail("config_invalid", "hedge_delay is invalid")
    val amount = match.groupValues[1].toLongOrNull() ?: fail("config_invalid", "hedge_delay is invalid")
    val millis = if (match.groupValues[2] == "s") amount * 1_000 else amount
    if (millis !in 0..30_000) fail("config_invalid", "hedge_delay is outside the supported range")
    return millis
}
