import {
  sqliteTable,
  text,
  integer,
  real,
  index,
  unique,
} from "drizzle-orm/sqlite-core";
import { relations, sql } from "drizzle-orm";
import crypto from "crypto";

// Local development uses SQLite. Timestamps are stored as epoch milliseconds
// so Drizzle maps them to Date instances consistently across the app.
const id = () => text("id").$defaultFn(() => crypto.randomUUID()).primaryKey();
const ts = (name: string) => integer(name, { mode: "timestamp_ms" });
const tsNotNull = (name: string) => integer(name, { mode: "timestamp_ms" }).notNull();
const tsDefault = (name: string) => integer(name, { mode: "timestamp_ms" }).notNull().defaultNow();
const bool = (name: string) => integer(name, { mode: "boolean" });
const serialId = () => integer("id").primaryKey({ autoIncrement: true });
const fkId = (name: string) => text(name);

// ========== 用户 ==========
export const users = sqliteTable("users", {
  id: id(),
  username: text("username").notNull().unique(),
  email: text("email").notNull().unique(),
  passwordHash: text("password_hash"),
  githubId: text("github_id").unique(),
  githubLogin: text("github_login"),
  githubAvatarUrl: text("github_avatar_url"),
  role: text("role").notNull().default("user"),
  referralCode: text("referral_code").unique(),
  referredBy: text("referred_by"),
  createdAt: tsDefault("created_at"),
});

export const usersRelations = relations(users, ({ many, one }) => ({
  agents: many(agents),
  accessTokens: many(accessTokens),
  orders: many(orders),
  subscription: one(subscriptions),
  refreshTokens: many(refreshTokens),
  passwordResetCodes: many(passwordResetCodes),
  referralsMade: many(referrals, { relationName: "referrer" }),
  referralsReceived: many(referrals, { relationName: "invitee" }),
  fnConfig: one(userFnConfigs),
  snippets: many(userSnippets),
  pathBookmarks: many(userPathBookmarks),
  override: one(userOverrides),
}));

// ========== 用户特权覆盖 ==========
export interface UserOverridesData {
  maxServers?: number;
  maxAgents?: number;
  relayBandwidthKbps?: number;
  allowRelayTransfer?: boolean;
}

export const userOverrides = sqliteTable("user_overrides", {
  id: id(),
  userId: fkId("user_id").notNull().unique().references(() => users.id, { onDelete: "cascade" }),
  overrides: text("overrides", { mode: "json" }).$type<UserOverridesData>().notNull().$defaultFn(() => ({})),
  note: text("note"),
  expiresAt: ts("expires_at"),
  createdAt: tsDefault("created_at"),
  updatedAt: tsDefault("updated_at"),
});

export const userOverridesRelations = relations(userOverrides, ({ one }) => ({
  user: one(users, { fields: [userOverrides.userId], references: [users.id] }),
}));

// ========== Hub 边缘节点 ==========
export const hubs = sqliteTable("hubs", {
  id: id(),
  name: text("name").notNull().default(""),
  region: text("region").notNull().default(""),
  httpUrl: text("http_url").notNull().default(""),
  grpcUrl: text("grpc_url").notNull().default(""),
  status: text("status").notNull().default("offline"),
  agentCount: integer("agent_count").notNull().default(0),
  bandwidthMbps: real("bandwidth_mbps").notNull().default(0),
  cpuCores: real("cpu_cores").notNull().default(0),
  memoryGb: real("memory_gb").notNull().default(0),
  maxAgents: integer("max_agents").notNull().default(0),
  lastHeartbeat: ts("last_heartbeat"),
  createdAt: tsDefault("created_at"),
}, (table) => [
  index("hubs_status_idx").on(table.status),
]);

export const hubsRelations = relations(hubs, ({ many }) => ({
  agents: many(agents),
}));

// ========== 连接密钥 ==========
export const accessTokens = sqliteTable(
  "access_tokens",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    tokenHash: text("token_hash").notNull().unique(),
    token: text("token").default(""),
    expiresAt: ts("expires_at"),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("access_tokens_user_id_idx").on(table.userId),
  ]
);

export const accessTokensRelations = relations(accessTokens, ({ one, many }) => ({
  user: one(users, { fields: [accessTokens.userId], references: [users.id] }),
  agents: many(agents),
}));

// ========== CLI 浏览器登录 ==========
export const browserLoginCodes = sqliteTable(
  "device_login_codes",
  {
    id: id(),
    deviceCode: text("device_code").notNull().unique(),
    userCode: text("user_code").notNull().unique(),
    clientName: text("client_name").notNull().default(""),
    status: text("status").notNull().default("pending"),
    userId: fkId("user_id").references(() => users.id, { onDelete: "cascade" }),
    accessTokenId: fkId("access_token_id").references(() => accessTokens.id, { onDelete: "set null" }),
    expiresAt: tsNotNull("expires_at"),
    approvedAt: ts("approved_at"),
    consumedAt: ts("consumed_at"),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("device_login_codes_device_code_idx").on(table.deviceCode),
    index("device_login_codes_user_code_idx").on(table.userCode),
    index("device_login_codes_expires_at_idx").on(table.expiresAt),
    index("device_login_codes_user_id_idx").on(table.userId),
  ]
);

export const browserLoginCodesRelations = relations(browserLoginCodes, ({ one }) => ({
  user: one(users, { fields: [browserLoginCodes.userId], references: [users.id] }),
  accessToken: one(accessTokens, { fields: [browserLoginCodes.accessTokenId], references: [accessTokens.id] }),
}));

export const deviceLoginCodes = browserLoginCodes;
export const deviceLoginCodesRelations = browserLoginCodesRelations;

// ========== Agent / 服务器节点 ==========
export const agents = sqliteTable(
  "agents",
  {
    id: id(),
    userId: fkId("user_id").notNull().default("").references(() => users.id, { onDelete: "cascade" }),
    name: text("name").notNull().default(""),
    tokenId: fkId("token_id").references(() => accessTokens.id, { onDelete: "set null" }),
    hostname: text("hostname").notNull().default(""),
    osInfo: text("os_info").notNull().default(""),
    labels: text("labels").notNull().default(""),
    online: bool("online").notNull().default(false),
    hubId: fkId("hub_id").references(() => hubs.id),
    pendingKick: bool("pending_kick").notNull().default(false),
    allowRelayTransfer: bool("allow_relay_transfer").notNull().default(false),
    sessionBytesIn: integer("session_bytes_in"),
    sessionBytesOut: integer("session_bytes_out"),
    sessionStartedAt: ts("session_started_at"),
    lastSeen: ts("last_seen"),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("agents_user_id_idx").on(table.userId),
    index("agents_hub_id_idx").on(table.hubId),
    index("agents_token_id_idx").on(table.tokenId),
    index("agents_hub_online_idx").on(table.hubId, table.online),
  ]
);

export const agentsRelations = relations(agents, ({ one }) => ({
  user: one(users, { fields: [agents.userId], references: [users.id] }),
  hub: one(hubs, { fields: [agents.hubId], references: [hubs.id] }),
  token: one(accessTokens, { fields: [agents.tokenId], references: [accessTokens.id] }),
}));

// ========== 套餐计划 ==========
export const plans = sqliteTable("plans", {
  id: id(),
  name: text("name").notNull(),
  price: integer("price").notNull().default(0),
  priceYearly: integer("price_yearly").notNull().default(0),
  features: text("features").notNull().default("[]"),
  maxServers: integer("max_servers").notNull().default(2),
  maxAgents: integer("max_agents").notNull().default(2),
  relayBandwidthKbps: integer("relay_bandwidth_kbps").notNull().default(0), // 0=不限，单位 KB/s
  active: bool("active").notNull().default(true),
  createdAt: tsDefault("created_at"),
});

export const plansRelations = relations(plans, ({ many }) => ({
  orders: many(orders),
  subscriptions: many(subscriptions),
}));

// ========== 订单 ==========
export const orders = sqliteTable(
  "orders",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    planId: fkId("plan_id").notNull().references(() => plans.id),
    amount: integer("amount").notNull(),
    currency: text("currency").notNull().default("CNY"),
    billingCycle: text("billing_cycle").notNull().default("monthly"),
    status: text("status").notNull().default("pending"),
    paymentMethod: text("payment_method").notNull().default(""),
    promoCodeId: fkId("promo_code_id"),
    discountAmount: integer("discount_amount").notNull().default(0),
    paymentId: text("payment_id").notNull().default(""),
    createdAt: tsDefault("created_at"),
    paidAt: ts("paid_at"),
    expiresAt: ts("expires_at"),
  },
  (table) => [
    index("orders_user_id_idx").on(table.userId),
    index("orders_status_paid_at_idx").on(table.status, table.paidAt),
  ]
);

export const ordersRelations = relations(orders, ({ one, many }) => ({
  user: one(users, { fields: [orders.userId], references: [users.id] }),
  plan: one(plans, { fields: [orders.planId], references: [plans.id] }),
  promoCode: one(promoCodes, { fields: [orders.promoCodeId], references: [promoCodes.id] }),
  subscriptions: many(subscriptions),
}));

// ========== 订阅 ==========
export const subscriptions = sqliteTable("subscriptions", {
  id: id(),
  userId: fkId("user_id").notNull().unique().references(() => users.id, { onDelete: "cascade" }),
  planId: fkId("plan_id").notNull().references(() => plans.id),
  orderId: fkId("order_id").notNull().references(() => orders.id),
  status: text("status").notNull().default("active"),
  currentPeriodEnd: tsNotNull("current_period_end"),
  createdAt: tsDefault("created_at"),
}, (table) => [
  index("subscriptions_status_period_end_idx").on(table.status, table.currentPeriodEnd),
]);

export const subscriptionsRelations = relations(subscriptions, ({ one }) => ({
  user: one(users, { fields: [subscriptions.userId], references: [users.id] }),
  plan: one(plans, { fields: [subscriptions.planId], references: [plans.id] }),
  order: one(orders, { fields: [subscriptions.orderId], references: [orders.id] }),
}));

// ========== Refresh Token ==========
export const refreshTokens = sqliteTable(
  "refresh_tokens",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    tokenHash: text("token_hash").notNull().unique(),
    tokenLookup: text("token_lookup").unique(),
    deviceName: text("device_name").notNull().default(""),
    expiresAt: tsNotNull("expires_at"),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("refresh_tokens_user_id_idx").on(table.userId),
  ]
);

export const refreshTokensRelations = relations(refreshTokens, ({ one }) => ({
  user: one(users, { fields: [refreshTokens.userId], references: [users.id] }),
}));

// ========== 密码重置验证码 ==========
export const passwordResetCodes = sqliteTable(
  "password_reset_codes",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    code: text("code").notNull(),
    expiresAt: tsNotNull("expires_at"),
    used: bool("used").notNull().default(false),
    attempts: integer("attempts").notNull().default(0),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("password_reset_codes_user_id_idx").on(table.userId),
  ]
);

export const passwordResetCodesRelations = relations(passwordResetCodes, ({ one }) => ({
  user: one(users, { fields: [passwordResetCodes.userId], references: [users.id] }),
}));

// ========== 优惠码 ==========
export const promoCodes = sqliteTable("promo_codes", {
  id: id(),
  code: text("code").notNull().unique(),
  discountType: text("discount_type").notNull(), // "fixed" | "percent"
  discountValue: integer("discount_value").notNull(), // 分 or 1-100
  maxUses: integer("max_uses"),
  usedCount: integer("used_count").notNull().default(0),
  startsAt: ts("starts_at"),
  expiresAt: ts("expires_at"),
  active: bool("active").notNull().default(true),
  createdAt: tsDefault("created_at"),
});

export const promoCodesRelations = relations(promoCodes, ({ many }) => ({
  usages: many(promoCodeUsages),
  orders: many(orders),
}));

export const promoCodeUsages = sqliteTable(
  "promo_code_usages",
  {
    id: id(),
    promoCodeId: fkId("promo_code_id").notNull().references(() => promoCodes.id),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    orderId: fkId("order_id").notNull().references(() => orders.id),
    discountAmount: integer("discount_amount").notNull(),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("promo_code_usages_promo_code_id_idx").on(table.promoCodeId),
    index("promo_code_usages_user_id_idx").on(table.userId),
    unique("promo_code_usages_user_promo_unique").on(table.userId, table.promoCodeId),
  ]
);

export const promoCodeUsagesRelations = relations(promoCodeUsages, ({ one }) => ({
  promoCode: one(promoCodes, { fields: [promoCodeUsages.promoCodeId], references: [promoCodes.id] }),
  user: one(users, { fields: [promoCodeUsages.userId], references: [users.id] }),
  order: one(orders, { fields: [promoCodeUsages.orderId], references: [orders.id] }),
}));

// ========== 邀请推荐 ==========
export const referrals = sqliteTable(
  "referrals",
  {
    id: id(),
    referrerId: fkId("referrer_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    inviteeId: fkId("invitee_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    status: text("status").notNull().default("pending"), // 'pending' | 'completed'
    rewarded: bool("rewarded").notNull().default(false),
    rewardDays: integer("reward_days").notNull().default(3),
    createdAt: tsDefault("created_at"),
    rewardedAt: ts("rewarded_at"),
  },
  (table) => [
    index("referrals_referrer_id_idx").on(table.referrerId),
    index("referrals_invitee_id_idx").on(table.inviteeId),
    unique("referrals_referrer_invitee_unique").on(table.referrerId, table.inviteeId),
  ]
);

export const referralsRelations = relations(referrals, ({ one }) => ({
  referrer: one(users, { fields: [referrals.referrerId], references: [users.id], relationName: "referrer" }),
  invitee: one(users, { fields: [referrals.inviteeId], references: [users.id], relationName: "invitee" }),
}));

// ========== 用户快捷键配置 ==========
export const userFnConfigs = sqliteTable("user_fn_configs", {
  id: id(),
  userId: fkId("user_id").notNull().unique().references(() => users.id, { onDelete: "cascade" }),
  config: text("config").notNull().default("{}"),
  updatedAt: tsDefault("updated_at"),
  createdAt: tsDefault("created_at"),
});

export const userFnConfigsRelations = relations(userFnConfigs, ({ one }) => ({
  user: one(users, { fields: [userFnConfigs.userId], references: [users.id] }),
}));

// ========== 用户代码片段 ==========
export const userSnippets = sqliteTable(
  "user_snippets",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    title: text("title").notNull(),
    content: text("content").notNull(),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("user_snippets_user_id_idx").on(table.userId),
  ]
);

export const userSnippetsRelations = relations(userSnippets, ({ one }) => ({
  user: one(users, { fields: [userSnippets.userId], references: [users.id] }),
}));

// ========== 用户路径收藏 ==========
export const userPathBookmarks = sqliteTable(
  "user_path_bookmarks",
  {
    id: id(),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    agentId: fkId("agent_id").notNull().references(() => agents.id, { onDelete: "cascade" }),
    path: text("path").notNull(),
    name: text("name").notNull(),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("user_path_bookmarks_user_id_idx").on(table.userId),
    index("user_path_bookmarks_agent_id_idx").on(table.agentId),
  ]
);

export const userPathBookmarksRelations = relations(userPathBookmarks, ({ one }) => ({
  user: one(users, { fields: [userPathBookmarks.userId], references: [users.id] }),
  agent: one(agents, { fields: [userPathBookmarks.agentId], references: [agents.id] }),
}));

// ========== Agent 发布版本 ==========
export const agentReleases = sqliteTable(
  "agent_releases",
  {
    id: serialId(),
    version: text("version").notNull(),
    os: text("os").notNull().default("linux"),
    arch: text("arch").notNull().default("amd64"),
    downloadUrl: text("download_url").notNull(),
    mirrors: text("mirrors").notNull().default("[]"),
    sha256: text("sha256").notNull().default(""),
    changelog: text("changelog").notNull().default(""),
    forceUpdate: bool("force_update").notNull().default(false),
    active: bool("active").notNull().default(true),
    minAppVersion: text("min_app_version").notNull().default(""),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("agent_releases_os_arch_idx").on(table.os, table.arch),
  ]
);

// ========== App 发布版本 ==========
export const appReleases = sqliteTable(
  "app_releases",
  {
    id: serialId(),
    platform: text("platform").notNull().default("android"),
    type: text("type").notNull(),
    version: text("version").notNull(),
    versionCode: integer("version_code").notNull(),
    fileName: text("file_name").notNull(),
    fileSize: text("file_size").notNull().default("0"),
    fileHash: text("file_hash").notNull().default(""),
    downloadUrl: text("download_url").notNull().default(""),
    mirrors: text("mirrors").notNull().default("[]"),
    changelog: text("changelog").notNull().default(""),
    forceUpdate: bool("force_update").notNull().default(false),
    minAgentVersion: text("min_agent_version").notNull().default(""),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("app_releases_platform_type_version_code_idx").on(table.platform, table.type, table.versionCode),
  ]
);

// ========== 中转流量统计（会话级） ==========
export const relayTraffic = sqliteTable(
  "relay_traffic",
  {
    id: serialId(),
    sessionId: text("session_id"),
    agentId: fkId("agent_id").notNull().references(() => agents.id, { onDelete: "cascade" }),
    userId: fkId("user_id").notNull().references(() => users.id, { onDelete: "cascade" }),
    hubId: fkId("hub_id").notNull(),
    bytesIn: integer("bytes_in").notNull().default(0),
    bytesOut: integer("bytes_out").notNull().default(0),
    sessionType: text("session_type"),
    connectedAt: ts("connected_at"),
    disconnectedAt: ts("disconnected_at"),
    duration: integer("duration"),
    createdAt: tsDefault("created_at"),
  },
  (table) => [
    index("relay_traffic_agent_id_idx").on(table.agentId),
    index("relay_traffic_user_id_idx").on(table.userId),
    index("relay_traffic_session_id_idx").on(table.sessionId),
  ]
);

export const relayTrafficRelations = relations(relayTraffic, ({ one }) => ({
  agent: one(agents, { fields: [relayTraffic.agentId], references: [agents.id] }),
  user: one(users, { fields: [relayTraffic.userId], references: [users.id] }),
}));
