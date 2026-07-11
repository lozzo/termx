import type { RemoteRuntimeStorage } from '../core/transport'

export interface MachineSessionStore {
  getSessionToken(machineId: string): string | null
  getAnswerProofSecret(machineId: string): string | null
  getSessionExpiry(machineId: string): string | null
  saveSessionToken(machineId: string, token: string, expiresAt: string, answerProofSecret?: string | undefined): void
  clearSessionToken(machineId: string): void
}

export function createMachineSessionStore(
  storage: RemoteRuntimeStorage,
): MachineSessionStore {
  return {
    getSessionToken: (machineId) => {
      const token = storage.getItem(`termx.session.${machineId}.token`)
      if (!token) return null
      if (looksLikePairSessionID(token)) {
        storage.removeItem(`termx.session.${machineId}.token`)
        storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
        return null
      }
      const exp = storage.getItem(`termx.session.${machineId}.exp`)
      if (exp) {
        try {
          const expiresAt = new Date(exp)
          if (!Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) {
            storage.removeItem(`termx.session.${machineId}.token`)
            storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
            return null
          }
        } catch {
          // 日期解析失败，忽略 exp 检查（不清除 token）
        }
      }
      return token
    },
    getAnswerProofSecret: (machineId) => {
      const exp = storage.getItem(`termx.session.${machineId}.exp`)
      if (exp) {
        const expiresAt = new Date(exp)
        if (!Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) {
          storage.removeItem(`termx.session.${machineId}.token`)
          storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
          return null
        }
      }
      return storage.getItem(`termx.session.${machineId}.answerProofSecret`)
    },
    getSessionExpiry: (machineId) =>
      storage.getItem(`termx.session.${machineId}.exp`),
    saveSessionToken: (machineId, token, expiresAt, answerProofSecret) => {
      // pair_session_id 只用于领取授权，不能作为 runtime session_token 缓存。
      if (looksLikePairSessionID(token)) {
        throw new Error('pairing session id cannot be stored as a runtime session token')
      }
      storage.setItem(`termx.session.${machineId}.token`, token)
      storage.setItem(`termx.session.${machineId}.exp`, expiresAt)
      if (answerProofSecret?.trim()) {
        storage.setItem(`termx.session.${machineId}.answerProofSecret`, answerProofSecret.trim())
      } else {
        storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
      }
    },
    clearSessionToken: (machineId) => {
      storage.removeItem(`termx.session.${machineId}.token`)
      storage.removeItem(`termx.session.${machineId}.exp`)
      storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
    },
  }
}

function looksLikePairSessionID(value: string): boolean {
  return /^pair[_-]/i.test(value.trim())
}
