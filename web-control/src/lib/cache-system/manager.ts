import { type CacheLockManager } from "./lock";
import { lockKeys } from "./keys";
import { type CacheStore } from "./store";

const EMPTY_SENTINEL = { __cacheNull: true } as const;

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function isNullSentinel(value: unknown): value is typeof EMPTY_SENTINEL {
  return typeof value === "object" && value !== null && "__cacheNull" in value;
}

export interface GetOrLoadOptions<T> {
  key: string;
  ttlMs: number;
  loader: () => Promise<T>;
  negativeTtlMs?: number;
  lockTtlMs?: number;
  waitTimeoutMs?: number;
  pollIntervalMs?: number;
}

export class CacheManager {
  constructor(
    private readonly store: CacheStore,
    private readonly lockManager: CacheLockManager
  ) {}

  async get<T>(key: string): Promise<T | undefined> {
    const value = await this.store.get<T | typeof EMPTY_SENTINEL>(key);
    if (isNullSentinel(value)) {
      return null as T;
    }
    return value;
  }

  async set<T>(key: string, value: T, ttlMs: number): Promise<void> {
    await this.store.set(key, value, ttlMs);
  }

  async delete(key: string): Promise<void> {
    await this.store.delete(key);
  }

  async getOrLoad<T>({
    key,
    ttlMs,
    loader,
    negativeTtlMs = ttlMs,
    lockTtlMs = Math.max(2_000, Math.min(ttlMs, 10_000)),
    waitTimeoutMs = 3_000,
    pollIntervalMs = 50,
  }: GetOrLoadOptions<T>): Promise<T> {
    const cached = await this.get<T>(key);
    if (cached !== undefined) return cached;

    const lockHandle = await this.lockManager.acquire(lockKeys.forCacheKey(key), lockTtlMs);

    if (lockHandle) {
      try {
        const doubleChecked = await this.get<T>(key);
        if (doubleChecked !== undefined) return doubleChecked;

        const loaded = await loader();
        if (loaded === null) {
          await this.store.set(key, EMPTY_SENTINEL, negativeTtlMs);
          return loaded;
        }

        await this.store.set(key, loaded, ttlMs);
        return loaded;
      } finally {
        await this.lockManager.release(lockHandle);
      }
    }

    const deadline = Date.now() + waitTimeoutMs;
    while (Date.now() < deadline) {
      await sleep(pollIntervalMs);
      const waited = await this.get<T>(key);
      if (waited !== undefined) return waited;
    }

    const loaded = await loader();
    if (loaded === null) {
      await this.store.set(key, EMPTY_SENTINEL, negativeTtlMs);
      return loaded;
    }

    await this.store.set(key, loaded, ttlMs);
    return loaded;
  }
}
