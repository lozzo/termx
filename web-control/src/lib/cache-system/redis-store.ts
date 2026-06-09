import { createClient } from "redis";
import { type CacheStore } from "./store";

type RedisClient = ReturnType<typeof createClient>;

let redisClientPromise: Promise<RedisClient> | null = null;

async function getRedisClient(): Promise<RedisClient> {
  if (!process.env.REDIS_URL) {
    throw new Error("REDIS_URL is required when CACHE_DRIVER=redis");
  }

  if (!redisClientPromise) {
    const client = createClient({ url: process.env.REDIS_URL });
    redisClientPromise = client.connect().then(() => client);
  }

  return redisClientPromise;
}

export class RedisCacheStore implements CacheStore {
  async get<T>(key: string): Promise<T | undefined> {
    const client = await getRedisClient();
    const raw = await client.get(key);
    if (raw === null) return undefined;
    return JSON.parse(raw) as T;
  }

  async set<T>(key: string, value: T, ttlMs: number): Promise<void> {
    const client = await getRedisClient();
    await client.set(key, JSON.stringify(value), {
      PX: ttlMs,
    });
  }

  async delete(key: string): Promise<void> {
    const client = await getRedisClient();
    await client.del(key);
  }

  async clear(): Promise<void> {
    throw new Error("RedisCacheStore.clear is not supported");
  }
}
