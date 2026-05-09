package com.termx.app.transfer

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper

internal data class PersistedTransfer(
    val id: String,
    val direction: String,
    val machineId: String,
    val name: String,
    val totalSize: Long,
    val transferredSize: Long,
    val status: String,
    val startedAt: Long,
    val updatedAt: Long,
    val remotePath: String,
    val localUri: String,
    val targetDir: String,
    val resumeId: String,
    val chunkSize: Int,
    val savedPath: String?,
    val savedUri: String?,
    val error: String?,
    val pausedByUser: Boolean,
)

internal class TransferTaskStore(context: Context) :
    SQLiteOpenHelper(context.applicationContext, DB_NAME, null, DB_VERSION) {

    companion object {
        private const val DB_NAME = "termx_file_transfers.db"
        private const val DB_VERSION = 1
        private const val TABLE = "transfers"
    }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS $TABLE (
                id TEXT PRIMARY KEY,
                direction TEXT NOT NULL,
                machine_id TEXT NOT NULL,
                name TEXT NOT NULL,
                total_size INTEGER NOT NULL,
                transferred_size INTEGER NOT NULL,
                status TEXT NOT NULL,
                started_at INTEGER NOT NULL,
                updated_at INTEGER NOT NULL,
                remote_path TEXT NOT NULL DEFAULT '',
                local_uri TEXT NOT NULL DEFAULT '',
                target_dir TEXT NOT NULL DEFAULT '',
                resume_id TEXT NOT NULL DEFAULT '',
                chunk_size INTEGER NOT NULL DEFAULT 65536,
                saved_path TEXT,
                saved_uri TEXT,
                error TEXT,
                paused_by_user INTEGER NOT NULL DEFAULT 1
            )
            """.trimIndent(),
        )
        db.execSQL("CREATE INDEX IF NOT EXISTS idx_${TABLE}_machine ON $TABLE(machine_id)")
        db.execSQL("CREATE INDEX IF NOT EXISTS idx_${TABLE}_updated ON $TABLE(updated_at)")
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        onCreate(db)
    }

    fun loadAll(): List<PersistedTransfer> {
        val rows = mutableListOf<PersistedTransfer>()
        readableDatabase.rawQuery(
            """
            SELECT id, direction, machine_id, name, total_size, transferred_size,
                   status, started_at, updated_at, remote_path, local_uri,
                   target_dir, resume_id, chunk_size, saved_path, saved_uri,
                   error, paused_by_user
            FROM $TABLE
            ORDER BY updated_at DESC, started_at DESC
            """.trimIndent(),
            emptyArray(),
        ).use { cursor ->
            while (cursor.moveToNext()) {
                rows.add(
                    PersistedTransfer(
                        id = cursor.getString(0),
                        direction = cursor.getString(1),
                        machineId = cursor.getString(2),
                        name = cursor.getString(3),
                        totalSize = cursor.getLong(4),
                        transferredSize = cursor.getLong(5),
                        status = cursor.getString(6),
                        startedAt = cursor.getLong(7),
                        updatedAt = cursor.getLong(8),
                        remotePath = cursor.getString(9),
                        localUri = cursor.getString(10),
                        targetDir = cursor.getString(11),
                        resumeId = cursor.getString(12),
                        chunkSize = cursor.getInt(13),
                        savedPath = cursor.getString(14),
                        savedUri = cursor.getString(15),
                        error = cursor.getString(16),
                        pausedByUser = cursor.getInt(17) != 0,
                    ),
                )
            }
        }
        return rows
    }

    fun upsert(record: PersistedTransfer) {
        val values = ContentValues().apply {
            put("id", record.id)
            put("direction", record.direction)
            put("machine_id", record.machineId)
            put("name", record.name)
            put("total_size", record.totalSize)
            put("transferred_size", record.transferredSize)
            put("status", record.status)
            put("started_at", record.startedAt)
            put("updated_at", record.updatedAt)
            put("remote_path", record.remotePath)
            put("local_uri", record.localUri)
            put("target_dir", record.targetDir)
            put("resume_id", record.resumeId)
            put("chunk_size", record.chunkSize)
            put("saved_path", record.savedPath)
            put("saved_uri", record.savedUri)
            put("error", record.error)
            put("paused_by_user", if (record.pausedByUser) 1 else 0)
        }
        writableDatabase.insertWithOnConflict(TABLE, null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun delete(id: String) {
        writableDatabase.delete(TABLE, "id = ?", arrayOf(id))
    }
}
