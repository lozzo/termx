package com.anytty.app

import android.app.Activity
import android.content.Intent
import android.database.Cursor
import android.net.Uri
import android.provider.OpenableColumns
import android.util.Base64
import android.util.Log
import androidx.activity.result.ActivityResult
import com.getcapacitor.JSArray
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.ActivityCallback
import com.getcapacitor.annotation.CapacitorPlugin

/**
 * NativeFilePickerPlugin — 使用 Android SAF (ACTION_OPEN_DOCUMENT) 选择文件
 *
 * 返回 content:// URI，供 WebView 读取并通过 Go resource stream 上传。
 */
@CapacitorPlugin(name = "NativeFilePicker")
class NativeFilePickerPlugin : Plugin() {

    companion object {
        private const val TAG = "AnyTTYNativeFilePicker"
    }

    @PluginMethod
    fun pickFiles(call: PluginCall) {
        val multiple = call.getBoolean("multiple", false) ?: false

        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            addFlags(Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
            type = "*/*"
            if (multiple) {
                putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
            }
        }

        startActivityForResult(call, intent, "pickFilesResult")
    }

    /** saveFile 在 Go resource stream 完整校验后把下载内容明确提交到 Android Downloads/AnyTTY。 */
    @PluginMethod
    fun saveFile(call: PluginCall) {
        val name = call.getString("name").orEmpty()
        val mimeType = call.getString("mimeType") ?: "application/octet-stream"
        val encoded = call.getString("dataBase64").orEmpty()
        try {
            val bytes = Base64.decode(encoded, Base64.DEFAULT)
            val saved = AndroidDownloadStore(context.applicationContext).save(name, mimeType, bytes)
            call.resolve(JSObject().apply {
                put("uri", saved.uri.toString())
                put("path", saved.path)
                put("bytes", saved.bytes)
                put("sha256", saved.sha256)
            })
        } catch (failure: Exception) {
            Log.e(TAG, "saveFile failed", failure)
            call.reject("failed to save download: ${failure.message}", failure)
        }
    }

    @ActivityCallback
    private fun pickFilesResult(call: PluginCall, result: ActivityResult) {
        if (result.resultCode != Activity.RESULT_OK) {
            val ret = JSObject()
            ret.put("files", JSArray())
            call.resolve(ret)
            return
        }

        val data = result.data
        val uris = mutableListOf<Uri>()

        if (data != null) {
            val clipData = data.clipData
            if (clipData != null) {
                for (i in 0 until clipData.itemCount) {
                    clipData.getItemAt(i).uri?.let { uris.add(it) }
                }
            } else {
                data.data?.let { uris.add(it) }
            }
        }

        val files = JSArray()
        for (uri in uris) {
            try {
                context.contentResolver.takePersistableUriPermission(
                    uri,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION,
                )
            } catch (e: Exception) {
                Log.w(TAG, "takePersistableUriPermission failed for $uri", e)
            }
            val file = queryFileInfo(uri)
            if (file != null) {
                files.put(file)
            }
        }

        val ret = JSObject()
        ret.put("files", files)
        call.resolve(ret)
    }

    private fun queryFileInfo(uri: Uri): JSObject? {
        var name = "unknown"
        var size = 0L
        var mimeType = "application/octet-stream"

        try {
            val cursor: Cursor? = context.contentResolver.query(uri, null, null, null, null)
            cursor?.use {
                if (it.moveToFirst()) {
                    val nameIdx = it.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    if (nameIdx >= 0) {
                        name = it.getString(nameIdx) ?: name
                    }
                    val sizeIdx = it.getColumnIndex(OpenableColumns.SIZE)
                    if (sizeIdx >= 0) {
                        size = it.getLong(sizeIdx)
                    }
                }
            }
            context.contentResolver.getType(uri)?.let { mimeType = it }
        } catch (e: Exception) {
            Log.e(TAG, "queryFileInfo error for $uri", e)
        }

        return JSObject().apply {
            put("uri", uri.toString())
            put("name", name)
            put("size", size)
            put("mimeType", mimeType)
        }
    }
}
