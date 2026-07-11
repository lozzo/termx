import type { RtcSession } from '../core/transport'
import {
  CORE_V2_TERMINAL_METHODS,
  assertLiveCacheOnlyAPIName,
  coreV2HistoryCopyRequestToProtocolRequest,
  coreV2HistoryWindowFromAPI,
  coreV2HistoryWindowRequestToParams,
  type CoreV2HistoryCopyRequest,
  type CoreV2HistoryWindow,
  type CoreV2HistoryWindowRequest,
} from './coreV2TerminalProtocol'

export interface CoreV2HistorySource {
  window(request: CoreV2HistoryWindowRequest): Promise<CoreV2HistoryWindow>
  copy(request: CoreV2HistoryCopyRequest): Promise<string>
}

export function createCoreV2HistorySource(
  session: Pick<RtcSession, 'openApi' | 'getConnectionInfo'>,
  machineId: string,
): CoreV2HistorySource {
  return {
    async window(request) {
      assertLiveCacheOnlyAPIName(CORE_V2_TERMINAL_METHODS.historyWindow)
      const info = await session.getConnectionInfo()
      if (info.machineId !== machineId) {
        throw new Error(`history source session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
      }

      const channel = await session.openApi()
      try {
        // copy/search/history 的权威数据只能来自 core-v2 logical-line history.window。
        const response = await channel.request<unknown>(
          CORE_V2_TERMINAL_METHODS.historyWindow,
          coreV2HistoryWindowRequestToParams(request),
        )
        return coreV2HistoryWindowFromAPI(response)
      } finally {
        channel.close()
      }
    },
    async copy(request) {
      assertLiveCacheOnlyAPIName(CORE_V2_TERMINAL_METHODS.historyCopy)
      const info = await session.getConnectionInfo()
      if (info.machineId !== machineId) {
        throw new Error(`history source session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
      }

      const protocolRequest = coreV2HistoryCopyRequestToProtocolRequest(request)
      const channel = await session.openApi()
      try {
        // 复制最终文本必须由 core-v2 frozen logical-line snapshot 生成。
        const response = await channel.request<unknown>(protocolRequest.method, protocolRequest.params)
        return historyCopyTextFromAPI(response)
      } finally {
        channel.close()
      }
    },
  }
}

function historyCopyTextFromAPI(value: unknown): string {
  if (typeof value === 'string') return value
  if (value instanceof Uint8Array) return new TextDecoder().decode(value)
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('history.copy response must be text')
  }
  const record = value as Record<string, unknown>
  if (typeof record.text === 'string') return record.text
  const bytes = record.bytes
  if (bytes instanceof Uint8Array) return new TextDecoder().decode(bytes)
  if (ArrayBuffer.isView(bytes)) {
    return new TextDecoder().decode(new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength))
  }
  if (Array.isArray(bytes) && bytes.every((item) => typeof item === 'number')) {
    return new TextDecoder().decode(new Uint8Array(bytes))
  }
  throw new Error('history.copy response must include text or bytes')
}
