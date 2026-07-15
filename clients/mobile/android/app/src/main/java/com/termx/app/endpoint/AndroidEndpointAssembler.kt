package com.termx.app.endpoint

import java.security.MessageDigest
import java.util.TreeMap

/**
 * AndroidEndpointAssembler 是 Official App 的纯 Endpoint 合并事务。
 * 只有完整 DeviceFingerprint + DeviceID 可以合并；label、地址、Cloud account 和裸 DeviceID 永远不能覆盖 pin。
 */
object AndroidEndpointAssembler {
    /** assemble 返回新的 registry snapshot，不执行网络、secure-store 写入或 runtime session 切换。 */
    fun assemble(
        registry: SavedEndpointRegistry,
        candidates: List<AndroidEndpointCandidate>,
        confirmedIdentityBindings: List<ConfirmedEndpointIdentityBinding> = emptyList(),
    ): AndroidEndpointAssemblerResult {
        validateRegistry(registry)
        val endpoints = registry.endpoints.mapValuesTo(linkedMapOf()) { (_, endpoint) -> endpoint.deepCopy() }
        var defaultEndpointId = registry.defaultEndpointId
        val resolved = MutableList(candidates.size) { "" }
        val credentialDescriptors = mutableListOf<AndroidCredentialDescriptor>()
        val candidateIdentities = candidates.map { candidate ->
            if (candidate.source == EndpointSource.LAN) fail("config_invalid", "LAN candidate cannot enter persistent assembler")
            candidate.identity.validate(true)
            validateCandidateClientPolicy(candidate)
            identityKey(candidate.identity)
        }.toSet()
        applyConfirmedIdentityBindings(endpoints, confirmedIdentityBindings, candidateIdentities)
        validateRegistry(SavedEndpointRegistry(ENDPOINT_REGISTRY_VERSION, defaultEndpointId, endpoints))
        val ordered = candidates.mapIndexed { index, candidate -> index to candidate }.sortedWith(
            compareBy<Pair<Int, AndroidEndpointCandidate>>(
                { it.second.identity.deviceFingerprint },
                { it.second.source.rank },
                { candidateSortKey(it.second) },
            ),
        )
        ordered.forEach { (originalIndex, candidate) ->
            val existing = findEndpoint(endpoints, candidate.identity)
            val endpointId = existing?.endpointId ?: deriveEndpointId(candidate.identity.deviceFingerprint, endpoints)
            val applyPolicy = candidateOwnsClientPolicy(candidate)
            var endpoint = existing ?: SavedEndpoint(
                endpointId = endpointId,
                label = candidate.suggestedLabel.trim().ifEmpty { candidate.identity.deviceId },
                labelSource = candidate.source,
                daemonIdentity = candidate.identity,
                routes = emptyMap(),
            )
            if (existing != null && applyPolicy && endpoint.labelSource != EndpointSource.USER && candidate.source.rank >= endpoint.labelSource.rank && candidate.suggestedLabel.isNotBlank()) {
                endpoint = endpoint.copy(label = candidate.suggestedLabel.trim(), labelSource = candidate.source)
            }
            if (applyPolicy && candidate.connectMode != null) {
                endpoint = endpoint.copy(connectMode = candidate.connectMode)
            }
            if (applyPolicy && candidate.selectionPolicy != null) {
                endpoint = endpoint.copy(selectionPolicy = candidate.selectionPolicy)
            }
            val routes = endpoint.routes.mapValuesTo(linkedMapOf()) { (_, route) -> route.copyLists() }
            candidate.routes.sortedBy(SavedAccessRoute::routeId).forEach { incoming ->
                if (incoming.source != candidate.source || incoming.policySource != candidate.source) {
                    fail("config_invalid", "route source does not match candidate source")
                }
                val route = incoming.copy(
                    source = incoming.source,
                    policySource = incoming.policySource,
                    hostKeyFingerprints = incoming.hostKeyFingerprints.sorted(),
                    addresses = incoming.addresses.sorted(),
                )
                val current = routes[route.routeId]
                if (current != null && current.kind != route.kind) fail("route_conflict", "route kind changed")
                routes[route.routeId] = when {
                    current != null -> mergeRoute(current, route, applyPolicy, candidate.source == EndpointSource.SHARE && candidate.applyClientPolicy)
                    !applyPolicy -> {
                        // 外部来源只提供配置；默认 policy 归客户端本地。已有分组策略时，新 route 等用户显式纳入竞速。
                        route.copy(
                            manualOnly = routes.values.any { it.enabled && !it.manualOnly && it.priority != null },
                            policySource = EndpointSource.LOCAL,
                        )
                    }
                    else -> route
                }
            }
            endpoint = endpoint.copy(routes = routes)
            endpoints[endpointId] = endpoint
            if (defaultEndpointId.isBlank()) defaultEndpointId = endpointId
            validateRegistry(SavedEndpointRegistry(ENDPOINT_REGISTRY_VERSION, defaultEndpointId, endpoints))
            resolved[originalIndex] = endpointId
            candidate.credentialDescriptors.forEach { descriptor ->
                validateCredentialDescriptor(descriptor)
                credentialDescriptors += descriptor
            }
        }
        return AndroidEndpointAssemblerResult(
            SavedEndpointRegistry(ENDPOINT_REGISTRY_VERSION, defaultEndpointId, TreeMap(endpoints)),
            resolved,
            normalizeCredentialDescriptors(credentialDescriptors),
        )
    }

    private fun findEndpoint(endpoints: Map<String, SavedEndpoint>, identity: EndpointDaemonIdentity): SavedEndpoint? {
        var fingerprintMatch: SavedEndpoint? = null
        endpoints.values.forEach { endpoint ->
            val current = endpoint.daemonIdentity
            if (current.isEmpty()) return@forEach
            if (current.deviceFingerprint == identity.deviceFingerprint) {
                if (current.deviceId != identity.deviceId) fail("identity_conflict", "fingerprint is pinned to another device_id")
                fingerprintMatch = endpoint
            }
            if (current.deviceId == identity.deviceId && current.deviceFingerprint != identity.deviceFingerprint) {
                fail("identity_conflict", "device_id is pinned to another fingerprint")
            }
        }
        return fingerprintMatch?.deepCopy()
    }

    private fun applyConfirmedIdentityBindings(
        endpoints: MutableMap<String, SavedEndpoint>,
        bindings: List<ConfirmedEndpointIdentityBinding>,
        candidateIdentities: Set<String>,
    ) {
        val seenEndpoints = mutableSetOf<String>()
        val seenIdentities = mutableSetOf<String>()
        bindings.sortedWith(compareBy(ConfirmedEndpointIdentityBinding::endpointId, { identityKey(it.identity) })).forEach { binding ->
            validateIdentifier("confirmed identity binding endpoint", binding.endpointId)
            binding.identity.validate(true)
            val key = identityKey(binding.identity)
            if (key !in candidateIdentities) fail("config_invalid", "confirmed identity binding has no matching verified candidate")
            if (!seenEndpoints.add(binding.endpointId)) fail("identity_conflict", "endpoint has multiple confirmed identity bindings")
            if (!seenIdentities.add(key)) fail("identity_conflict", "daemon identity is confirmed for multiple endpoints")
            val endpoint = endpoints[binding.endpointId] ?: fail("config_invalid", "confirmed identity binding endpoint does not exist")
            if (!endpoint.daemonIdentity.isEmpty()) {
                if (endpoint.daemonIdentity != binding.identity) fail("identity_conflict", "endpoint is already pinned to another daemon identity")
                return@forEach
            }
            val existing = findEndpoint(endpoints, binding.identity)
            if (existing != null && existing.endpointId != binding.endpointId) {
                fail("identity_conflict", "daemon identity is already pinned to another endpoint")
            }
            endpoints[binding.endpointId] = endpoint.copy(daemonIdentity = binding.identity)
        }
    }

    private fun identityKey(identity: EndpointDaemonIdentity): String = "${identity.deviceId}\u0000${identity.deviceFingerprint}"

    private fun mergeRoute(existing: SavedAccessRoute, incoming: SavedAccessRoute, applyPolicy: Boolean, forcePolicy: Boolean): SavedAccessRoute {
        var merged = existing.copyLists()
        if (incoming.source.rank >= existing.source.rank) {
            merged = incoming.copy(
                enabled = existing.enabled,
                manualOnly = existing.manualOnly,
                priority = existing.priority,
                policySource = existing.policySource,
            )
        }
        if (applyPolicy && (forcePolicy || incoming.policySource.rank >= existing.policySource.rank)) {
            merged = merged.copy(
                enabled = incoming.enabled,
                manualOnly = incoming.manualOnly,
                priority = incoming.priority,
                policySource = incoming.policySource,
            )
        }
        return merged
    }

    private fun deriveEndpointId(fingerprint: String, endpoints: Map<String, SavedEndpoint>): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(fingerprint.trim().toByteArray())
        val encoded = digest.joinToString("") { "%02x".format(it) }
        for (length in 12..encoded.length step 4) {
            val candidate = "daemon-${encoded.substring(0, length)}"
            if (!endpoints.containsKey(candidate)) return candidate
        }
        return "daemon-$encoded"
    }

    private fun candidateSortKey(candidate: AndroidEndpointCandidate): String = buildString {
        append(candidate.identity.deviceId).append('\u0000')
        append(candidate.identity.deviceFingerprint).append('\u0000')
        append(candidate.source.wireName).append('\u0000')
        append(candidate.suggestedLabel.trim()).append('\u0000')
        append(candidate.connectMode?.wireName.orEmpty()).append('\u0000')
        append(candidate.selectionPolicy).append('\u0000').append(candidate.applyClientPolicy)
        candidate.routes.sortedWith(compareBy(SavedAccessRoute::routeId, { it.kind.wireName })).forEach { route ->
            append('\u0000').append(route.copyLists())
        }
        candidate.credentialDescriptors.sortedWith(compareBy(AndroidCredentialDescriptor::descriptorId, { it.kind.wireName })).forEach { descriptor ->
            append('\u0000').append(descriptor)
        }
    }

    private fun validateCandidateClientPolicy(candidate: AndroidEndpointCandidate) {
        if (candidate.applyClientPolicy && candidate.source != EndpointSource.SHARE) {
            fail("config_invalid", "only a confirmed share candidate may apply imported client policy")
        }
        val ownsPolicy = candidateOwnsClientPolicy(candidate)
        if ((candidate.connectMode != null || candidate.selectionPolicy != null) && !ownsPolicy) {
            fail("config_invalid", "candidate source cannot change client policy")
        }
        candidate.selectionPolicy?.let { policy ->
            if (!policy.hedgeDelayConfigured && policy.hedgeDelayMillis != 0L) {
                fail("config_invalid", "candidate hedge delay must be zero when it is not configured")
            }
            if (policy.hedgeDelayConfigured && policy.hedgeDelayMillis !in 0..30_000) {
                fail("config_invalid", "candidate hedge delay is outside the supported range")
            }
        }
        if (!ownsPolicy && candidate.routes.any { !it.enabled || it.manualOnly || it.priority != null }) {
            fail("config_invalid", "candidate source cannot import route selection policy")
        }
    }

    private fun candidateOwnsClientPolicy(candidate: AndroidEndpointCandidate): Boolean = when (candidate.source) {
        EndpointSource.LOCAL, EndpointSource.MANUAL, EndpointSource.USER -> true
        EndpointSource.SHARE -> candidate.applyClientPolicy
        EndpointSource.LAN, EndpointSource.CLOUD, EndpointSource.BOOTSTRAP -> false
    }

    private fun normalizeCredentialDescriptors(values: List<AndroidCredentialDescriptor>): List<AndroidCredentialDescriptor> {
        val normalized = mutableListOf<AndroidCredentialDescriptor>()
        values.sortedWith(compareBy(AndroidCredentialDescriptor::descriptorId, { it.kind.wireName })).forEach { value ->
            val existing = normalized.lastOrNull { it.descriptorId == value.descriptorId }
            if (existing != null) {
                if (existing.kind != value.kind || existing.exportable != value.exportable) {
                    fail("config_invalid", "credential descriptor is defined inconsistently")
                }
            } else {
                normalized += value
            }
        }
        return normalized
    }
}

private fun SavedEndpoint.deepCopy(): SavedEndpoint = copy(routes = routes.mapValues { (_, route) -> route.copyLists() })
private fun SavedAccessRoute.copyLists(): SavedAccessRoute = copy(hostKeyFingerprints = hostKeyFingerprints.toList(), addresses = addresses.toList())
