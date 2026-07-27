import { create } from '@bufbuild/protobuf'
import type { ProtoClientSession } from '../core/protoClientSession'
import { CommandEnvelopeSchema } from '../generated/apipb/application_pb'
import {
  HistoryCopyCommandSchema,
  HistoryCursorSchema,
  HistoryReleaseCommandSchema,
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
  type CoreV2HistoryReleaseRequest,
  type CoreV2HistoryWindow,
  type CoreV2HistoryWindowRequest,
} from './coreV2TerminalProtocol'

export interface CoreV2HistorySource {
  window(request: CoreV2HistoryWindowRequest, options?: { signal?: AbortSignal }): Promise<CoreV2HistoryWindow>
  copy(request: CoreV2HistoryCopyRequest, options?: { signal?: AbortSignal }): Promise<string>
  release?(
    request: CoreV2HistoryReleaseRequest & { generation?: string | number | bigint | undefined },
    options?: { signal?: AbortSignal },
  ): Promise<void>
}

export function createCoreV2HistorySource(
  session: ProtoClientSession,
  machineId: string,
): CoreV2HistorySource {
  return createProtoHistorySource(session, machineId)
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
    async window(request, options) {
      const command = windowCommand(request)
      const result = await session.execute(
        create(CommandEnvelopeSchema, { command: { case: 'historyWindow', value: command } }),
        options,
      )
      if (result.result.case !== 'historyWindow') throw new Error('history window returned no result')
      return coreV2HistoryWindowFromAPI(result.result.value)
    },
    async copy(request, options) {
      const command = create(HistoryCopyCommandSchema, {
        terminal: terminal(request.terminalId),
        window: windowCommand({ ...request, mode: 'range', limit: 1 }),
      })
      const result = await session.execute(
        create(CommandEnvelopeSchema, { command: { case: 'historyCopy', value: command } }),
        options,
      )
      if (result.result.case !== 'historyCopy') throw new Error('history copy returned no result')
      return result.result.value.text
    },
    async release(request, options) {
      await session.execute(
        create(CommandEnvelopeSchema, {
          command: {
            case: 'historyRelease',
            value: create(HistoryReleaseCommandSchema, {
              terminal: terminal(request.terminalId),
              token: request.token,
              historyGeneration: BigInt(request.generation ?? 0),
            }),
          },
        }),
        options,
      )
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
