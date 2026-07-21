package com.muxvia.app

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import java.io.File
import java.security.MessageDigest

/**
 * AndroidDownloadStore 是 Android 下载落盘的唯一平台 primitive。
 * 远端读取、摘要验证和 transfer lifecycle 仍由 Go/JS resource stream 拥有；这里只在完整内容到达后提交本地文件。
 */
class AndroidDownloadStore(private val context: Context) {
    data class SavedDownload(val uri: Uri, val path: String, val bytes: Long, val sha256: String)

    /** save 原子写入 Downloads/Muxvia，并返回可由测试和 UI 校验的真实持久化结果。 */
    fun save(rawName: String, mimeType: String, bytes: ByteArray): SavedDownload {
        val name = safeName(rawName)
        val digest = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveMediaStore(name, mimeType, bytes, digest)
        else saveLegacy(name, bytes, digest)
    }

    private fun saveMediaStore(name: String, mimeType: String, bytes: ByteArray, digest: String): SavedDownload {
        val resolver = context.contentResolver
        val relativePath = Environment.DIRECTORY_DOWNLOADS + "/Muxvia"
        resolver.query(
            MediaStore.Downloads.EXTERNAL_CONTENT_URI,
            arrayOf(MediaStore.Downloads._ID),
            "${MediaStore.Downloads.DISPLAY_NAME}=? AND ${MediaStore.Downloads.RELATIVE_PATH}=?",
            arrayOf(name, "$relativePath/"),
            null,
        )?.use { cursor ->
            while (cursor.moveToNext()) {
                resolver.delete(Uri.withAppendedPath(MediaStore.Downloads.EXTERNAL_CONTENT_URI, cursor.getLong(0).toString()), null, null)
            }
        }
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, name)
            put(MediaStore.Downloads.MIME_TYPE, mimeType)
            put(MediaStore.Downloads.RELATIVE_PATH, relativePath)
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw IllegalStateException("Android MediaStore rejected download")
        try {
            resolver.openOutputStream(uri, "w")?.use { it.write(bytes) }
                ?: throw IllegalStateException("Android MediaStore output is unavailable")
            resolver.update(uri, ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }, null, null)
        } catch (failure: Throwable) {
            resolver.delete(uri, null, null)
            throw failure
        }
        return SavedDownload(uri, "Downloads/Muxvia/$name", bytes.size.toLong(), digest)
    }

    @Suppress("DEPRECATION")
    private fun saveLegacy(name: String, bytes: ByteArray, digest: String): SavedDownload {
        val directory = File(context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS), "Muxvia").apply { mkdirs() }
        val target = File(directory, name)
        target.outputStream().use { it.write(bytes) }
        return SavedDownload(Uri.fromFile(target), target.absolutePath, bytes.size.toLong(), digest)
    }

    private fun safeName(rawName: String): String {
        val name = rawName.trim()
        require(name.isNotEmpty() && name != "." && name != ".." && !name.contains('/') && !name.contains('\\') && !name.contains('\u0000')) {
            "download file name is invalid"
        }
        return name
    }
}
