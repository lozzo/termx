package com.anytty.app

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import java.security.MessageDigest

@RunWith(AndroidJUnit4::class)
class AndroidDownloadStoreInstrumentedTest {
    @Test
    fun savePersistsExactContentAndReportsDigest() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val content = ByteArray(128 * 1024) { index -> (index % 251).toByte() }
        val saved = AndroidDownloadStore(context).save("rtc010-download-store.bin", "application/octet-stream", content)
        val persisted = context.contentResolver.openInputStream(saved.uri)!!.use { it.readBytes() }
        val digest = MessageDigest.getInstance("SHA-256").digest(content).joinToString("") { "%02x".format(it) }

        assertArrayEquals(content, persisted)
        assertEquals(content.size.toLong(), saved.bytes)
        assertEquals(digest, saved.sha256)
        context.contentResolver.delete(saved.uri, null, null)
    }
}
