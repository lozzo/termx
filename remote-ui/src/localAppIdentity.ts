import type { LocalAgentApi, LocalPairResult } from './transport'
import type { LocalOfferSignature } from './localWebRtcTransport'

const storageKeys = {
  appDeviceId: 'termx.local.appDeviceId',
  appName: 'termx.local.appName',
  appPublicKey: 'termx.local.appPublicKey',
  appCertificate: 'termx.local.appCertificate',
} as const

export interface LocalAppIdentity {
  appDeviceId: string
  appName: string
  appPublicKey: string
}

export interface LocalAppIdentityStore {
  loadIdentity(): LocalAppIdentity | null
  saveIdentity(input: LocalAppIdentity): void
  loadCertificate(): string | null
  saveCertificate(value: string): void
}

export interface LocalAppCrypto {
  generateKeyPair(): Promise<{
    publicKey: { raw: Uint8Array }
    privateKey: unknown
  }>
  savePrivateKey(appDeviceId: string, privateKey: unknown): Promise<void>
  loadPrivateKey(appDeviceId: string): Promise<unknown | null>
  sign(privateKey: unknown, message: Uint8Array): Promise<Uint8Array>
  randomBytes(length: number): Promise<Uint8Array>
  sha256(data: Uint8Array): Promise<Uint8Array>
}

export interface EnsureLocalAppIdentityOptions {
  storage: LocalAppIdentityStore
  crypto: LocalAppCrypto
  appName: string
}

export interface LocalOfferSigningInput {
  sessionId: string
  machineId: string
  terminalId: string
  sdp: string
}

export interface CanonicalLocalOfferInput extends LocalOfferSigningInput {
  nonce: string
  timestamp: number
}

export interface LocalOfferSigner {
  signOffer(input: LocalOfferSigningInput): Promise<LocalOfferSignature>
}

export interface LocalOfferSignerOptions {
  storage: LocalAppIdentityStore
  crypto: LocalAppCrypto
  nonce?: (() => string | Promise<string>) | undefined
  now?: (() => Date) | undefined
}

export interface PairLocalAppOptions {
  api: Pick<LocalAgentApi, 'pair'>
  storage: LocalAppIdentityStore
  crypto: LocalAppCrypto
  appName: string
  pairSessionId: string
  pairSecret: string
}

export function createLocalAppIdentityStore(storage: Pick<Storage, 'getItem' | 'setItem'>): LocalAppIdentityStore {
  return {
    loadIdentity() {
      const appDeviceId = storage.getItem(storageKeys.appDeviceId)
      const appName = storage.getItem(storageKeys.appName)
      const appPublicKey = storage.getItem(storageKeys.appPublicKey)
      if (!appDeviceId || !appName || !appPublicKey) return null
      return {
        appDeviceId,
        appName,
        appPublicKey,
      }
    },
    saveIdentity(input) {
      storage.setItem(storageKeys.appDeviceId, input.appDeviceId)
      storage.setItem(storageKeys.appName, input.appName)
      storage.setItem(storageKeys.appPublicKey, input.appPublicKey)
    },
    loadCertificate() {
      return storage.getItem(storageKeys.appCertificate)
    },
    saveCertificate(value) {
      storage.setItem(storageKeys.appCertificate, value)
    },
  }
}

export async function ensureLocalAppIdentity(options: EnsureLocalAppIdentityOptions): Promise<LocalAppIdentity> {
  const existing = options.storage.loadIdentity()
  if (existing) {
    return {
      appDeviceId: existing.appDeviceId,
      appName: existing.appName,
      appPublicKey: existing.appPublicKey,
    }
  }
  const keyPair = await options.crypto.generateKeyPair()
  const appDeviceId = `appweb_${base64url(await options.crypto.randomBytes(16))}`
  const identity = {
    appDeviceId,
    appName: options.appName,
    appPublicKey: base64(keyPair.publicKey.raw),
  }
  await options.crypto.savePrivateKey(appDeviceId, keyPair.privateKey)
  options.storage.saveIdentity(identity)
  return {
    appDeviceId: identity.appDeviceId,
    appName: identity.appName,
    appPublicKey: identity.appPublicKey,
  }
}

export async function canonicalLocalOfferMessage(input: CanonicalLocalOfferInput): Promise<string> {
  const digest = await browserSHA256(new TextEncoder().encode(input.sdp))
  return [
    'termx-webrtc-offer-v1:',
    'ticket_id:',
    `machine_id:${input.machineId.trim()}`,
    `terminal_id:${input.terminalId.trim()}`,
    `sha256(sdp):${hex(digest)}`,
    `nonce:${input.nonce.trim()}`,
    `timestamp:${Math.trunc(input.timestamp)}`,
  ].join('\n')
}

export function createLocalOfferSigner(options: LocalOfferSignerOptions): LocalOfferSigner {
  return {
    async signOffer(input) {
      const identity = options.storage.loadIdentity()
      if (!identity) throw new Error('local app identity is required before signing an offer')
      if (!options.storage.loadCertificate()) {
        throw new Error('local app certificate is required before signing an offer')
      }
      const nonce = await (options.nonce?.() ?? randomNonce(options.crypto))
      const timestamp = Math.trunc((options.now?.() ?? new Date()).getTime() / 1000)
      const message = await canonicalLocalOfferMessageWithHash({
        ...input,
        nonce,
        timestamp,
      }, options.crypto)
      const privateKey = await options.crypto.loadPrivateKey(identity.appDeviceId)
      if (!privateKey) throw new Error('local app private key is required before signing an offer')
      const signature = await options.crypto.sign(privateKey, new TextEncoder().encode(message))
      return {
        signature: base64(signature),
        nonce,
        timestamp: String(timestamp),
      }
    },
  }
}

export async function pairLocalApp(options: PairLocalAppOptions): Promise<LocalPairResult> {
  const identity = await ensureLocalAppIdentity({
    storage: options.storage,
    crypto: options.crypto,
    appName: options.appName,
  })
  const result = await options.api.pair({
    pairSessionId: options.pairSessionId,
    pairSecret: options.pairSecret,
    appDeviceId: identity.appDeviceId,
    appName: identity.appName,
    appPublicKey: identity.appPublicKey,
    requestedCapabilities: ['terminal', 'file_manager'],
  })
  options.storage.saveCertificate(result.appCertificate)
  return result
}

export function createBrowserLocalAppCrypto(): LocalAppCrypto {
  const crypto = globalThis.crypto
  const subtle = crypto?.subtle
  if (!crypto || !subtle) {
    throw new Error('WebCrypto is required for local app identity')
  }
  return {
    async generateKeyPair() {
      const keyPair = await subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']) as CryptoKeyPair
      return {
        publicKey: { raw: new Uint8Array(await subtle.exportKey('raw', keyPair.publicKey)) },
        privateKey: keyPair.privateKey,
      }
    },
    async savePrivateKey(appDeviceId: string, privateKey: unknown) {
      await browserPrivateKeyStore().put(appDeviceId, privateKey as CryptoKey)
    },
    async loadPrivateKey(appDeviceId: string) {
      return browserPrivateKeyStore().get(appDeviceId)
    },
    async sign(privateKey: unknown, message: Uint8Array) {
      return new Uint8Array(await subtle.sign({ name: 'Ed25519' }, privateKey as CryptoKey, toArrayBuffer(message)))
    },
    async randomBytes(length: number) {
      const out = new Uint8Array(length)
      crypto.getRandomValues(out)
      return out
    },
    async sha256(data: Uint8Array) {
      return new Uint8Array(await subtle.digest('SHA-256', toArrayBuffer(data)))
    },
  }
}

async function randomNonce(crypto: LocalAppCrypto): Promise<string> {
  return base64url(await crypto.randomBytes(16))
}

async function canonicalLocalOfferMessageWithHash(input: CanonicalLocalOfferInput, crypto: Pick<LocalAppCrypto, 'sha256'>): Promise<string> {
  const digest = await crypto.sha256(new TextEncoder().encode(input.sdp))
  return [
    'termx-webrtc-offer-v1:',
    'ticket_id:',
    `machine_id:${input.machineId.trim()}`,
    `terminal_id:${input.terminalId.trim()}`,
    `sha256(sdp):${hex(digest)}`,
    `nonce:${input.nonce.trim()}`,
    `timestamp:${Math.trunc(input.timestamp)}`,
  ].join('\n')
}

async function browserSHA256(data: Uint8Array): Promise<Uint8Array> {
  if (!globalThis.crypto?.subtle) {
    throw new Error('crypto.subtle is required for local offer signing')
  }
  return new Uint8Array(await globalThis.crypto.subtle.digest('SHA-256', toArrayBuffer(data)))
}

function toArrayBuffer(data: Uint8Array): ArrayBuffer {
  const out = new ArrayBuffer(data.byteLength)
  new Uint8Array(out).set(data)
  return out
}

function base64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

function base64url(bytes: Uint8Array): string {
  return base64(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

function browserPrivateKeyStore() {
  if (!globalThis.indexedDB) {
    throw new Error('IndexedDB is required for local app private key storage')
  }
  return {
    async get(appDeviceId: string): Promise<CryptoKey | null> {
      const db = await openPrivateKeyDB()
      return transaction<CryptoKey | null>(db, 'readonly', (store, resolve, reject) => {
        const req = store.get(appDeviceId)
        req.onerror = () => reject(req.error ?? new Error('failed to load local app private key'))
        req.onsuccess = () => resolve((req.result as CryptoKey | undefined) ?? null)
      })
    },
    async put(appDeviceId: string, key: CryptoKey): Promise<void> {
      const db = await openPrivateKeyDB()
      return transaction<void>(db, 'readwrite', (store, resolve, reject) => {
        const req = store.put(key, appDeviceId)
        req.onerror = () => reject(req.error ?? new Error('failed to save local app private key'))
        req.onsuccess = () => resolve()
      })
    },
  }
}

function openPrivateKeyDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('termx-local-app-identity', 1)
    req.onerror = () => reject(req.error ?? new Error('failed to open local app identity store'))
    req.onupgradeneeded = () => {
      req.result.createObjectStore('app-private-keys')
    }
    req.onsuccess = () => resolve(req.result)
  })
}

function transaction<T>(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore, resolve: (value: T) => void, reject: (reason?: unknown) => void) => void,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction('app-private-keys', mode)
    tx.onerror = () => reject(tx.error ?? new Error('local app private key transaction failed'))
    run(tx.objectStore('app-private-keys'), resolve, reject)
  })
}
