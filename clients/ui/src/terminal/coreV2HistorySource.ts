import { create } from '@bufbuild/protobuf'
import type { ProtoClientSession } from '../core/protoClientSession'
import type { RtcSession } from '../core/transport'
import { CommandEnvelopeSchema } from '../generated/apipb/application_pb'
import {
  HistoryCopyCommandSchema,
  HistoryCursorSchema,
  HistoryRangeSchema,
  HistoryWindowCommandSchema,
  HistoryWindowMode,
} from '../generated/apipb/history_pb'
import { TerminalRefSchema } from '../generated/apipb/terminal_pb'
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
  session: Pick<RtcSession, 'openApi' | 'getConnectionInfo'> | ProtoClientSession,
  machineId: string,
): CoreV2HistorySource {
  if ('execute' in session) return createProtoHistorySource(session, machineId)
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

function createProtoHistorySource(session: ProtoClientSession, machineId: string): CoreV2HistorySource {
  if (session.stamp.endpointId !== machineId) {
    throw new Error(`history source session machine mismatch: connected to ${session.stamp.endpointId}, expected ${machineId}`)
  }
  const terminal = (terminalId: string) => create(TerminalRefSchema, { endpointId: machineId, terminalId })
  const windowCommand = (request: CoreV2HistoryWindowRequest) => create(HistoryWindowCommandSchema, {
    terminal: terminal(request.terminalId),
    mode: protoHistoryMode(request.mode),
    beforeOffset: request.beforeOffset ?? 0,
    limit: request.limit,
    cols: request.cols,
    token: request.token ?? '',
    historyGeneration: BigInt(request.generation ?? 0),
    beforeCursor: request.beforeCursor ? create(HistoryCursorSchema, { lineId: BigInt(request.beforeCursor.lineId), rowInLine: request.beforeCursor.rowInLine }) : undefined,
    afterCursor: request.afterCursor ? create(HistoryCursorSchema, { lineId: BigInt(request.afterCursor.lineId), rowInLine: request.afterCursor.rowInLine }) : undefined,
    boundaryFirstLineId: BigInt(request.boundaryFirstLineId ?? 0),
    boundaryLastLineId: BigInt(request.boundaryLastLineId ?? 0),
    range: request.range ? create(HistoryRangeSchema, {
      startLineId: BigInt(request.range.startLineId), startCol: request.range.startCol,
      endLineId: BigInt(request.range.endLineId), endCol: request.range.endCol,
    }) : undefined,
  })
  return {
    async window(request) {
      const command = windowCommand(request)
      const result = await session.execute(create(CommandEnvelopeSchema, { command: { case: 'historyWindow', value: command } }))
      if (result.result.case !== 'historyWindow') throw new Error('history window returned no result')
      return coreV2HistoryWindowFromAPI(result.result.value)
    },
    async copy(request) {
      const command = create(HistoryCopyCommandSchema, {
        terminal: terminal(request.terminalId),
        window: windowCommand({ ...request, mode: 'range', limit: 1 }),
      })
      const result = await session.execute(create(CommandEnvelopeSchema, { command: { case: 'historyCopy', value: command } }))
      if (result.result.case !== 'historyCopy') throw new Error('history copy returned no result')
      return result.result.value.text
    },
  }
}

function protoHistoryMode(mode: CoreV2HistoryWindowRequest['mode']): HistoryWindowMode {
  switch (mode) {
    case 'latest': return HistoryWindowMode.LATEST
    case 'older': return HistoryWindowMode.OLDER
    case 'newer': return HistoryWindowMode.NEWER
    case 'oldest': return HistoryWindowMode.OLDEST
    case 'range': return HistoryWindowMode.LATEST
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
