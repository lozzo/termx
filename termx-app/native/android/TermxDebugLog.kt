package com.termx.app

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import android.webkit.ConsoleMessage
import androidx.core.content.FileProvider
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

object TermxDebugLog {
    private const val TAG = "TermxDebugLog"
    private const val LOG_DIR_NAME = "termx-debug-logs"
    private const val SHARE_DIR_NAME = "termx-debug-share"
    private const val CURRENT_LOG_NAME = "termx-debug.log"
    private const val MAX_LOG_BYTES = 4L * 1024L * 1024L
    private const val MAX_ROLLED_FILES = 4
    private const val MAX_MESSAGE_CHARS = 64 * 1024

    @Volatile private var appContext: Context? = null
    @Volatile private var initialized = false
    @Volatile private var logcatProcess: java.lang.Process? = null

    private val writeExecutor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "TermxDebugLog").apply { isDaemon = true }
    }
    private val fileLock = Object()
    private val timestampFormat = ThreadLocal.withInitial {
        SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US)
    }
    private val fileStampFormat = ThreadLocal.withInitial {
        SimpleDateFormat("yyyyMMdd-HHmmss", Locale.US)
    }

    @JvmStatic
    fun init(context: Context) {
        if (initialized) return
        synchronized(this) {
            if (initialized) return
            appContext = context.applicationContext
            logDir(context).mkdirs()
            initialized = true
            i(TAG, "debug log initialized package=${context.packageName} sdk=${Build.VERSION.SDK_INT}")
            startLogcatCapture()
        }
    }

    @JvmStatic
    fun i(tag: String, message: String) {
        write("INFO", tag, message, null)
    }

    @JvmStatic
    fun w(tag: String, message: String, throwable: Throwable? = null) {
        write("WARN", tag, message, throwable)
    }

    @JvmStatic
    fun e(tag: String, message: String, throwable: Throwable? = null) {
        write("ERROR", tag, message, throwable)
    }

    @JvmStatic
    fun console(consoleMessage: ConsoleMessage?) {
        if (consoleMessage == null) return
        val level = consoleMessage.messageLevel()?.name ?: "LOG"
        val source = consoleMessage.sourceId()?.takeLast(120).orEmpty()
        val message = consoleMessage.message().orEmpty()
        write(
            level = level,
            tag = "WebViewConsole",
            message = "source=$source line=${consoleMessage.lineNumber()} message=${sanitize(message)}",
            throwable = null,
        )
    }

    @JvmStatic
    fun exportAndShare(activity: Activity): File {
        val archive = exportArchive(activity)
        val uri = FileProvider.getUriForFile(
            activity,
            "${activity.packageName}.fileprovider",
            archive,
        )
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "application/zip"
            putExtra(Intent.EXTRA_STREAM, uri)
            putExtra(Intent.EXTRA_SUBJECT, "TermX debug logs")
            putExtra(Intent.EXTRA_TEXT, "TermX debug log export: ${archive.name}")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        activity.startActivity(Intent.createChooser(intent, "Share TermX logs"))
        i(TAG, "shared debug log archive name=${archive.name} bytes=${archive.length()}")
        return archive
    }

    @JvmStatic
    fun exportArchive(context: Context): File {
        init(context)
        i(TAG, "export requested")
        flush(2000L)

        val shareDir = File(context.cacheDir, SHARE_DIR_NAME)
        shareDir.mkdirs()
        shareDir.listFiles()?.forEach { file ->
            if (file.isFile && file.name.startsWith("termx-debug-") && file.name.endsWith(".zip")) {
                file.delete()
            }
        }

        val archive = File(shareDir, "termx-debug-${fileStampFormat.get()!!.format(Date())}.zip")
        ZipOutputStream(FileOutputStream(archive)).use { zip ->
            addTextEntry(zip, "metadata.json", buildMetadata(context).toString(2))
            val files = logDir(context)
                .listFiles()
                ?.filter { it.isFile && it.name.startsWith("termx-debug") }
                ?.sortedBy { it.lastModified() }
                ?: emptyList()
            for (file in files) {
                addFileEntry(zip, "logs/${file.name}", file)
            }
            addTextEntry(zip, "logcat-tail.txt", collectLogcatTail())
        }
        return archive
    }

    @JvmStatic
    fun clear(context: Context) {
        init(context)
        flush(2000L)
        synchronized(fileLock) {
            logDir(context).listFiles()?.forEach { file ->
                if (file.isFile && file.name.startsWith("termx-debug")) file.delete()
            }
        }
        i(TAG, "logs cleared")
    }

    private fun write(level: String, tag: String, message: String, throwable: Throwable?) {
        val context = appContext ?: return
        val now = timestampFormat.get()!!.format(Date())
        val safeMessage = sanitize(message)
        val suffix = throwable?.let { " throwable=${sanitize(Log.getStackTraceString(it))}" }.orEmpty()
        val line = "$now $level/$tag: $safeMessage$suffix\n"
        writeExecutor.execute {
            appendLine(context, line)
        }
    }

    private fun appendLine(context: Context, line: String) {
        synchronized(fileLock) {
            val dir = logDir(context)
            dir.mkdirs()
            val current = File(dir, CURRENT_LOG_NAME)
            if (current.exists() && current.length() >= MAX_LOG_BYTES) {
                rotateLogs(dir)
            }
            FileOutputStream(current, true).use { stream ->
                stream.write(line.toByteArray(Charsets.UTF_8))
            }
        }
    }

    private fun rotateLogs(dir: File) {
        for (index in MAX_ROLLED_FILES downTo 1) {
            val source = if (index == 1) File(dir, CURRENT_LOG_NAME) else File(dir, "$CURRENT_LOG_NAME.${index - 1}")
            val target = File(dir, "$CURRENT_LOG_NAME.$index")
            if (!source.exists()) continue
            if (target.exists()) target.delete()
            source.renameTo(target)
        }
    }

    private fun flush(timeoutMs: Long) {
        try {
            writeExecutor.submit { }.get(timeoutMs, TimeUnit.MILLISECONDS)
        } catch (_: Exception) {
            // Export should still proceed with whatever has reached disk.
        }
    }

    private fun startLogcatCapture() {
        if (logcatProcess != null) return
        val context = appContext ?: return
        try {
            val pid = android.os.Process.myPid().toString()
            val process = ProcessBuilder("logcat", "--pid", pid, "-v", "threadtime")
                .redirectErrorStream(true)
                .start()
            logcatProcess = process
            Thread({
                try {
                    process.inputStream.bufferedReader(Charsets.UTF_8).useLines { lines ->
                        lines.forEach { appendLine(context, "${timestampFormat.get()!!.format(Date())} LOGCAT: $it\n") }
                    }
                } catch (e: Exception) {
                    w(TAG, "logcat capture read failed: ${e.message}")
                } finally {
                    logcatProcess = null
                }
            }, "TermxLogcatCapture").apply {
                isDaemon = true
                start()
            }
            i(TAG, "logcat capture started pid=$pid")
        } catch (e: Exception) {
            w(TAG, "logcat capture unavailable: ${e.message}")
        }
    }

    private fun collectLogcatTail(): String {
        val pid = android.os.Process.myPid().toString()
        return runLogcatCommand(listOf("logcat", "-d", "--pid", pid, "-t", "3000", "-v", "threadtime"))
            ?: runLogcatCommand(listOf("logcat", "-d", "-t", "1000", "-v", "threadtime"))
            ?: "logcat unavailable\n"
    }

    private fun runLogcatCommand(command: List<String>): String? {
        return try {
            val process = ProcessBuilder(command).redirectErrorStream(true).start()
            val text = process.inputStream.bufferedReader(Charsets.UTF_8).readText()
            if (!process.waitFor(2, TimeUnit.SECONDS)) {
                process.destroy()
                return null
            }
            text
        } catch (_: Exception) {
            null
        }
    }

    private fun addTextEntry(zip: ZipOutputStream, name: String, text: String) {
        zip.putNextEntry(ZipEntry(name))
        zip.write(text.toByteArray(Charsets.UTF_8))
        zip.closeEntry()
    }

    private fun addFileEntry(zip: ZipOutputStream, name: String, file: File) {
        zip.putNextEntry(ZipEntry(name))
        file.inputStream().use { input -> input.copyTo(zip) }
        zip.closeEntry()
    }

    private fun buildMetadata(context: Context): JSONObject {
        val packageInfo = try {
            context.packageManager.getPackageInfo(context.packageName, 0)
        } catch (_: Exception) {
            null
        }
        return JSONObject()
            .put("packageName", context.packageName)
            .put("versionName", packageInfo?.versionName ?: "")
            .put("sdkInt", Build.VERSION.SDK_INT)
            .put("manufacturer", Build.MANUFACTURER)
            .put("model", Build.MODEL)
            .put("processId", android.os.Process.myPid())
            .put("exportedAt", timestampFormat.get()!!.format(Date()))
    }

    private fun logDir(context: Context): File {
        return File(context.cacheDir, LOG_DIR_NAME)
    }

    private fun sanitize(value: String): String {
        val normalized = value.replace('\n', ' ').replace('\r', ' ')
        return if (normalized.length <= MAX_MESSAGE_CHARS) {
            normalized
        } else {
            normalized.take(MAX_MESSAGE_CHARS) + "...<truncated ${normalized.length - MAX_MESSAGE_CHARS} chars>"
        }
    }
}
