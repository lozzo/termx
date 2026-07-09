import { CacheManager } from "./manager";
import { MemoryLockManager } from "./memory-lock";
import { MemoryCacheStore } from "./memory-store";
import { RedisLockManager } from "./redis-lock";
import { RedisCacheStore } from "./redis-store";

const driver = process.env.CACHE_DRIVER || "memory";

const cacheStore = driver === "redis" ? new RedisCacheStore() : new MemoryCacheStore();
const cacheLockManager = driver === "redis" ? new RedisLockManager() : new MemoryLockManager();

export const cacheManager = new CacheManager(cacheStore, cacheLockManager);
export { cacheStore, cacheLockManager };
