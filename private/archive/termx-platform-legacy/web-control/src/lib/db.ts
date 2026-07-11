import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import fs from "fs";
import path from "path";
import * as schema from "./schema";
import { ensureSqliteSchema } from "./sqlite-bootstrap";

function createDrizzleClient() {
  const sqlitePath = process.env.SQLITE_PATH || process.env.DATABASE_URL || "./data/termx-web.sqlite";
  const filename = sqlitePath.startsWith("file:") ? sqlitePath.slice("file:".length) : sqlitePath;
  const resolvedFilename = path.resolve(process.cwd(), filename);
  if (resolvedFilename !== ":memory:") {
    fs.mkdirSync(path.dirname(resolvedFilename), { recursive: true });
  }

  const sqlite = new Database(resolvedFilename);
  sqlite.pragma("journal_mode = WAL");
  sqlite.pragma("foreign_keys = ON");
  ensureSqliteSchema(sqlite);
  return drizzle(sqlite, { schema });
}

const globalForDrizzle = globalThis as unknown as {
  db: ReturnType<typeof createDrizzleClient> | undefined;
};

export const db = globalForDrizzle.db ?? createDrizzleClient();

// 所有环境都缓存到 globalThis，防止 Next.js HMR 或多 worker 重复创建 Pool
globalForDrizzle.db = db;
