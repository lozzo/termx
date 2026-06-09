import crypto from "crypto";
import { createClient } from "redis";
import { type CacheLockManager, type LockHandle } from "./lock";

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

const RELEASE_SCRIPT = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`;

export class RedisLockManager implements CacheLockManager {
  async acquire(key: string, ttlMs: number): Promise<LockHandle | null> {
    const client = await getRedisClient();
    const token = crypto.randomUUID();
    const result = await client.set(key, token, {
      NX: true,
      PX: ttlMs,
    });

    if (result !== "OK") {
      return null;
    }

    return { key, token };
  }

  async release(handle: LockHandle): Promise<void> {
    const client = await getRedisClient();
    await client.eval(RELEASE_SCRIPT, {
      keys: [handle.key],
      arguments: [handle.token],
    });
  }
}
