package com.termx.app.goclient

/** GoClientNative 是 Android 到稳定 C ABI 的薄 JNI 门面，不持有网络、认证、协议或重连状态。 */
object GoClientNative {
    init {
        System.loadLibrary("termx_client_jni")
    }

    external fun abiVersion(): Int
    external fun create(runtimeDir: String): Long
    external fun openSession(engine: Long, requestProto: ByteArray): Long
    external fun execute(engine: Long, session: Long, commandProto: ByteArray): Long
    external fun nextEvent(engine: Long, timeoutMillis: Int): ByteArray
    external fun cancel(engine: Long, operation: Long)
    external fun closeSession(engine: Long, session: Long)
    external fun release(engine: Long, handle: Long)
    external fun close(engine: Long)
}
