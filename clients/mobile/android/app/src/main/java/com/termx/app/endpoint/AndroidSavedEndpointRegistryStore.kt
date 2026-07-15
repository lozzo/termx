package com.termx.app.endpoint

import java.io.File
import java.io.FileOutputStream

/**
 * AndroidSavedEndpointRegistryStore 是 Official App 的普通 endpoint 配置持久化 owner。
 * 它只保存 Endpoint/Route 期望状态；credential body、Cloud token、CapabilityGrant 与 runtime session 必须留在各自安全 owner。
 */
class AndroidSavedEndpointRegistryStore(private val file: File) {
    /** load 在文件缺失时返回空 v2 registry；损坏、未知字段或旧版本直接失败，不做 Cloud/legacy fallback。 */
    @Synchronized
    fun load(): SavedEndpointRegistry = if (!file.exists()) SavedEndpointRegistry() else EndpointRegistryCodec.decode(file.readBytes())

    /** save 先完整校验并同步临时文件，再原子替换旧 registry；失败时保留旧文件。 */
    @Synchronized
    fun save(registry: SavedEndpointRegistry) {
        val payload = EndpointRegistryCodec.encode(registry)
        val parent = file.parentFile ?: fail("config_invalid", "endpoint registry parent is missing")
        if (!parent.exists() && !parent.mkdirs()) fail("config_invalid", "endpoint registry directory cannot be created")
        val temporary = File.createTempFile(".endpoints-", ".json", parent)
        var published = false
        try {
            FileOutputStream(temporary).use { output ->
                output.write(payload)
                output.fd.sync()
            }
            temporary.setReadable(false, false)
            temporary.setWritable(false, false)
            temporary.setReadable(true, true)
            temporary.setWritable(true, true)
            if (!temporary.renameTo(file)) fail("config_invalid", "endpoint registry cannot be published")
            published = true
        } finally {
            payload.fill(0)
            if (!published) temporary.delete()
        }
    }
}
