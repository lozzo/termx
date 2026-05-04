import crypto from "crypto";
import { type CacheLockManager, type LockHandle } from "./lock";

interface MemoryLockEntry {
  token: string;
  expiresAt: number;
}

export class MemoryLockManager implements CacheLockManager {
  private locks = new Map<string, MemoryLockEntry>();

  async acquire(key: string, ttlMs: number): Promise<LockHandle | null> {
    const now = Date.now();
    const current = this.locks.get(key);
    if (current && current.expiresAt > now) {
      return null;
    }

    const token = crypto.randomUUID();
    this.locks.set(key, {
      token,
      expiresAt: now + ttlMs,
    });

    return { key, token };
  }

  async release(handle: LockHandle): Promise<void> {
    const current = this.locks.get(handle.key);
    if (!current) return;
    if (current.token !== handle.token) return;
    this.locks.delete(handle.key);
  }
}
