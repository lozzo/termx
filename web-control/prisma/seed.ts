import "dotenv/config";
import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import fs from "fs";
import path from "path";
import { plans } from "../src/lib/schema";
import { ensureSqliteSchema } from "../src/lib/sqlite-bootstrap";

const sqlitePath = process.env.SQLITE_PATH || process.env.DATABASE_URL || "./data/termx-web.sqlite";
const filename = sqlitePath.startsWith("file:") ? sqlitePath.slice("file:".length) : sqlitePath;
const resolvedFilename = path.resolve(process.cwd(), filename);
fs.mkdirSync(path.dirname(resolvedFilename), { recursive: true });
const sqlite = new Database(resolvedFilename);
ensureSqliteSchema(sqlite);
const db = drizzle(sqlite);

async function main() {
  console.log("Seeding database...");

  // 创建默认套餐计划 (upsert)
  const freePlan = await db
    .insert(plans)
    .values({
      id: "free",
      name: "社区版",
      price: 0,
      priceYearly: 0,
      features: JSON.stringify([
        "无限本地会话",
        "P2P 直连",
        "完整移动端 App 访问",
        "社区支持",
      ]),
      maxServers: 2,
      maxAgents: 2,
      active: true,
    })
    .onConflictDoUpdate({
      target: plans.id,
      set: { name: "社区版" },
    })
    .returning();

  const proPlan = await db
    .insert(plans)
    .values({
      id: "pro",
      name: "Pro 版",
      price: 900,
      priceYearly: 9900,
      features: JSON.stringify([
        "包含社区版所有功能",
        "全球高速 Hub 中转节点",
        "100% 连接可靠性",
        "云端节点状态看板",
        "优先技术支持",
      ]),
      maxServers: 20,
      maxAgents: 20,
      active: true,
    })
    .onConflictDoUpdate({
      target: plans.id,
      set: { name: "Pro 版" },
    })
    .returning();

  console.log("Plans seeded:", { freePlan, proPlan });
  console.log("Seeding complete.");
}

main()
  .catch((e) => {
    console.error(e);
    process.exit(1);
  })
  .finally(() => {
    sqlite.close();
  });
