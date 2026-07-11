import type Database from "better-sqlite3";

let initialized = false;

const schemaSql = `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY NOT NULL,
  username TEXT NOT NULL UNIQUE,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT,
  github_id TEXT UNIQUE,
  github_login TEXT,
  github_avatar_url TEXT,
  role TEXT NOT NULL DEFAULT 'user',
  referral_code TEXT UNIQUE,
  referred_by TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS user_overrides (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  overrides TEXT NOT NULL DEFAULT '{}',
  note TEXT,
  expires_at INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
  updated_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS hubs (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  region TEXT NOT NULL DEFAULT '',
  http_url TEXT NOT NULL DEFAULT '',
  grpc_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'offline',
  agent_count INTEGER NOT NULL DEFAULT 0,
  bandwidth_mbps REAL NOT NULL DEFAULT 0,
  cpu_cores REAL NOT NULL DEFAULT 0,
  memory_gb REAL NOT NULL DEFAULT 0,
  max_agents INTEGER NOT NULL DEFAULT 0,
  last_heartbeat INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS hubs_status_idx ON hubs(status);

CREATE TABLE IF NOT EXISTS access_tokens (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  token TEXT DEFAULT '',
  expires_at INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS access_tokens_user_id_idx ON access_tokens(user_id);

CREATE TABLE IF NOT EXISTS device_login_codes (
  id TEXT PRIMARY KEY NOT NULL,
  device_code TEXT NOT NULL UNIQUE,
  user_code TEXT NOT NULL UNIQUE,
  client_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
  access_token_id TEXT REFERENCES access_tokens(id) ON DELETE SET NULL,
  expires_at INTEGER NOT NULL,
  approved_at INTEGER,
  consumed_at INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS device_login_codes_device_code_idx ON device_login_codes(device_code);
CREATE INDEX IF NOT EXISTS device_login_codes_user_code_idx ON device_login_codes(user_code);
CREATE INDEX IF NOT EXISTS device_login_codes_expires_at_idx ON device_login_codes(expires_at);
CREATE INDEX IF NOT EXISTS device_login_codes_user_id_idx ON device_login_codes(user_id);

CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL DEFAULT '' REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT '',
  token_id TEXT REFERENCES access_tokens(id) ON DELETE SET NULL,
  hostname TEXT NOT NULL DEFAULT '',
  os_info TEXT NOT NULL DEFAULT '',
  labels TEXT NOT NULL DEFAULT '',
  online INTEGER NOT NULL DEFAULT 0,
  hub_id TEXT REFERENCES hubs(id),
  pending_kick INTEGER NOT NULL DEFAULT 0,
  allow_relay_transfer INTEGER NOT NULL DEFAULT 0,
  session_bytes_in INTEGER,
  session_bytes_out INTEGER,
  session_started_at INTEGER,
  last_seen INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS agents_user_id_idx ON agents(user_id);
CREATE INDEX IF NOT EXISTS agents_hub_id_idx ON agents(hub_id);
CREATE INDEX IF NOT EXISTS agents_token_id_idx ON agents(token_id);
CREATE INDEX IF NOT EXISTS agents_hub_online_idx ON agents(hub_id, online);

CREATE TABLE IF NOT EXISTS plans (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  price INTEGER NOT NULL DEFAULT 0,
  price_yearly INTEGER NOT NULL DEFAULT 0,
  features TEXT NOT NULL DEFAULT '[]',
  max_servers INTEGER NOT NULL DEFAULT 2,
  max_agents INTEGER NOT NULL DEFAULT 2,
  relay_bandwidth_kbps INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS orders (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_id TEXT NOT NULL REFERENCES plans(id),
  amount INTEGER NOT NULL,
  currency TEXT NOT NULL DEFAULT 'CNY',
  billing_cycle TEXT NOT NULL DEFAULT 'monthly',
  status TEXT NOT NULL DEFAULT 'pending',
  payment_method TEXT NOT NULL DEFAULT '',
  promo_code_id TEXT,
  discount_amount INTEGER NOT NULL DEFAULT 0,
  payment_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
  paid_at INTEGER,
  expires_at INTEGER
);

CREATE INDEX IF NOT EXISTS orders_user_id_idx ON orders(user_id);
CREATE INDEX IF NOT EXISTS orders_status_paid_at_idx ON orders(status, paid_at);

CREATE TABLE IF NOT EXISTS subscriptions (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  plan_id TEXT NOT NULL REFERENCES plans(id),
  order_id TEXT NOT NULL REFERENCES orders(id),
  status TEXT NOT NULL DEFAULT 'active',
  current_period_end INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS subscriptions_status_period_end_idx ON subscriptions(status, current_period_end);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  token_lookup TEXT UNIQUE,
  device_name TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS refresh_tokens_user_id_idx ON refresh_tokens(user_id);

CREATE TABLE IF NOT EXISTS password_reset_codes (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS password_reset_codes_user_id_idx ON password_reset_codes(user_id);

CREATE TABLE IF NOT EXISTS promo_codes (
  id TEXT PRIMARY KEY NOT NULL,
  code TEXT NOT NULL UNIQUE,
  discount_type TEXT NOT NULL,
  discount_value INTEGER NOT NULL,
  max_uses INTEGER,
  used_count INTEGER NOT NULL DEFAULT 0,
  starts_at INTEGER,
  expires_at INTEGER,
  active INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS promo_code_usages (
  id TEXT PRIMARY KEY NOT NULL,
  promo_code_id TEXT NOT NULL REFERENCES promo_codes(id),
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  order_id TEXT NOT NULL REFERENCES orders(id),
  discount_amount INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
  UNIQUE(user_id, promo_code_id)
);

CREATE INDEX IF NOT EXISTS promo_code_usages_promo_code_id_idx ON promo_code_usages(promo_code_id);
CREATE INDEX IF NOT EXISTS promo_code_usages_user_id_idx ON promo_code_usages(user_id);

CREATE TABLE IF NOT EXISTS referrals (
  id TEXT PRIMARY KEY NOT NULL,
  referrer_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invitee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  rewarded INTEGER NOT NULL DEFAULT 0,
  reward_days INTEGER NOT NULL DEFAULT 3,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
  rewarded_at INTEGER,
  UNIQUE(referrer_id, invitee_id)
);

CREATE INDEX IF NOT EXISTS referrals_referrer_id_idx ON referrals(referrer_id);
CREATE INDEX IF NOT EXISTS referrals_invitee_id_idx ON referrals(invitee_id);

CREATE TABLE IF NOT EXISTS user_fn_configs (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  config TEXT NOT NULL DEFAULT '{}',
  updated_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS user_snippets (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS user_snippets_user_id_idx ON user_snippets(user_id);

CREATE TABLE IF NOT EXISTS user_path_bookmarks (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS user_path_bookmarks_user_id_idx ON user_path_bookmarks(user_id);
CREATE INDEX IF NOT EXISTS user_path_bookmarks_agent_id_idx ON user_path_bookmarks(agent_id);

CREATE TABLE IF NOT EXISTS agent_releases (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL DEFAULT 'linux',
  arch TEXT NOT NULL DEFAULT 'amd64',
  download_url TEXT NOT NULL,
  mirrors TEXT NOT NULL DEFAULT '[]',
  sha256 TEXT NOT NULL DEFAULT '',
  changelog TEXT NOT NULL DEFAULT '',
  force_update INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  min_app_version TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS agent_releases_os_arch_idx ON agent_releases(os, arch);

CREATE TABLE IF NOT EXISTS app_releases (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  platform TEXT NOT NULL DEFAULT 'android',
  type TEXT NOT NULL,
  version TEXT NOT NULL,
  version_code INTEGER NOT NULL,
  file_name TEXT NOT NULL,
  file_size TEXT NOT NULL DEFAULT '0',
  file_hash TEXT NOT NULL DEFAULT '',
  download_url TEXT NOT NULL DEFAULT '',
  mirrors TEXT NOT NULL DEFAULT '[]',
  changelog TEXT NOT NULL DEFAULT '',
  force_update INTEGER NOT NULL DEFAULT 0,
  min_agent_version TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS app_releases_platform_type_version_code_idx ON app_releases(platform, type, version_code);

CREATE TABLE IF NOT EXISTS relay_traffic (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  session_id TEXT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hub_id TEXT NOT NULL,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  session_type TEXT,
  connected_at INTEGER,
  disconnected_at INTEGER,
  duration INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS relay_traffic_agent_id_idx ON relay_traffic(agent_id);
CREATE INDEX IF NOT EXISTS relay_traffic_user_id_idx ON relay_traffic(user_id);
CREATE INDEX IF NOT EXISTS relay_traffic_session_id_idx ON relay_traffic(session_id);
`;

const seedSql = `
INSERT INTO plans (
  id, name, price, price_yearly, features, max_servers, max_agents, relay_bandwidth_kbps, active
) VALUES (
  'free',
  '社区版',
  0,
  0,
  '["无限本地会话","P2P 直连","完整移动端 App 访问","社区支持"]',
  2,
  2,
  0,
  1
) ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  active = excluded.active;

INSERT INTO plans (
  id, name, price, price_yearly, features, max_servers, max_agents, relay_bandwidth_kbps, active
) VALUES (
  'pro',
  'Pro 版',
  900,
  9900,
  '["包含社区版所有功能","全球高速 Hub 中转节点","100% 连接可靠性","云端节点状态看板","优先技术支持"]',
  20,
  20,
  0,
  1
) ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  active = excluded.active;
`;

export function ensureSqliteSchema(sqlite: Database.Database) {
  if (initialized) return;

  sqlite.exec(schemaSql);
  dropColumnIfExists(sqlite, "agents", "paired");
  sqlite.exec(seedSql);
  initialized = true;
}

function dropColumnIfExists(sqlite: Database.Database, table: string, column: string) {
  const rows = sqlite.prepare(`PRAGMA table_info(${table})`).all() as Array<{ name?: string }>;
  if (!rows.some((row) => row.name === column)) return;
  sqlite.exec(`ALTER TABLE ${table} DROP COLUMN ${column}`);
}
