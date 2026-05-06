import type { RemoteRuntimeStorage } from './transport'

export interface MachineSessionStore {
  getSessionToken(machineId: string): string | null
  saveSessionToken(machineId: string, token: string, expiresAt: string): void
  clearSessionToken(machineId: string): void
}

export function createMachineSessionStore(
  storage: RemoteRuntimeStorage,
): MachineSessionStore {
  return {
    getSessionToken: (machineId) =>
      storage.getItem(`termx.session.${machineId}.token`),
    saveSessionToken: (machineId, token, expiresAt) => {
      storage.setItem(`termx.session.${machineId}.token`, token)
      storage.setItem(`termx.session.${machineId}.exp`, expiresAt)
    },
    clearSessionToken: (machineId) => {
      storage.removeItem(`termx.session.${machineId}.token`)
      storage.removeItem(`termx.session.${machineId}.exp`)
    },
  }
}
