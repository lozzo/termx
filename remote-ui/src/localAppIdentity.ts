import type { RemoteRuntimeStorage } from './transport'

export interface MachineSessionStore {
  getSessionToken(machineId: string): string | null
  getAnswerProofSecret(machineId: string): string | null
  saveSessionToken(machineId: string, token: string, expiresAt: string, answerProofSecret?: string | undefined): void
  clearSessionToken(machineId: string): void
}

export function createMachineSessionStore(
  storage: RemoteRuntimeStorage,
): MachineSessionStore {
  return {
    getSessionToken: (machineId) =>
      storage.getItem(`termx.session.${machineId}.token`),
    getAnswerProofSecret: (machineId) =>
      storage.getItem(`termx.session.${machineId}.answerProofSecret`),
    saveSessionToken: (machineId, token, expiresAt, answerProofSecret) => {
      storage.setItem(`termx.session.${machineId}.token`, token)
      storage.setItem(`termx.session.${machineId}.exp`, expiresAt)
      if (answerProofSecret?.trim()) {
        storage.setItem(`termx.session.${machineId}.answerProofSecret`, answerProofSecret.trim())
      }
    },
    clearSessionToken: (machineId) => {
      storage.removeItem(`termx.session.${machineId}.token`)
      storage.removeItem(`termx.session.${machineId}.exp`)
      storage.removeItem(`termx.session.${machineId}.answerProofSecret`)
    },
  }
}
