export interface LockHandle {
  key: string;
  token: string;
}

export interface CacheLockManager {
  acquire(key: string, ttlMs: number): Promise<LockHandle | null>;
  release(handle: LockHandle): Promise<void>;
}
