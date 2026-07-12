import {
  create,
  fromBinary,
  toBinary,
  type DescMessage,
  type MessageInitShape,
  type MessageShape,
  type UnknownField,
} from '@bufbuild/protobuf'
import { WireType } from '@bufbuild/protobuf/wire'
import {
  AttachParamsSchema,
  AttachResultSchema,
  EmptySchema,
  EnsureResizeParamsSchema,
  EnsureResizeResultSchema,
  ErrorEnvelopeSchema,
  EventSchema,
  FileBatchResultSchema,
  FileCopyMoveParamsSchema,
  FileDownloadOpenParamsSchema,
  FileEntrySchema,
  FileListParamsSchema,
  FileListResultSchema,
  FileOperationResultSchema,
  FilePathParamsSchema,
  FilePreviewParamsSchema,
  FilePreviewResultSchema,
  FileRenameParamsSchema,
  FileTransferAckSchema,
  FileTransferCancelParamsSchema,
  FileTransferCancelResultSchema,
  FileTransferDataSchema,
  FileTransferFinishSchema,
  FileTransferOpenResultSchema,
  FileTransferResultSchema,
  FileUploadOpenParamsSchema,
  GetParamsSchema,
  GridViewportSchema,
  HelloSchema,
  ListResultSchema,
  RequestEnvelopeSchema,
  ResponseEnvelopeSchema,
  SnapshotSchema,
  TerminalInfoSchema,
  type CursorState,
  type Event,
  type FileEntry,
  type GridViewport,
  type ResizeControl,
  type ResizeOwnership,
  type RowSet,
  type Snapshot,
  type TerminalInfo,
  type TerminalModes,
} from '../generated/wirepb/terminal_pb'

export interface TerminalRequestEnvelope {
  id: number
  method: string
  params: Uint8Array
}

export interface TerminalResponseEnvelope {
  id: number
  result: Uint8Array
}

export interface TerminalErrorEnvelope {
  id: number
  code: number
  message: string
}

export interface TerminalFileTransferData { offset: number; data: Uint8Array }
export interface TerminalFileTransferAck { offset: number; windowBytes: number }
export interface TerminalFileTransferFinish { size: number; sha256: Uint8Array }
export interface TerminalFileTransferResult { path: string; size: number; sha256: Uint8Array }

interface CompactCell {
  content: string
  width: number
  style: CompactStyle
  linkUrl: string
  linkParams: string
}

interface CompactStyle {
  fg: string
  bg: string
  bold: boolean
  italic: boolean
  underline: boolean
  blink: boolean
  reverse: boolean
  strikethrough: boolean
  linkUrl: string
  linkParams: string
}

const emptyStyle: CompactStyle = {
  fg: '',
  bg: '',
  bold: false,
  italic: false,
  underline: false,
  blink: false,
  reverse: false,
  strikethrough: false,
  linkUrl: '',
  linkParams: '',
}

const compactRowsMagic = 'TXS2'
const compactRowMagic = 'TXR2'
const compactRowFlagRuns = 1 << 0
const compactRowFlagCells = 1 << 1
const compactRowStyleFlag = 1 << 0
const compactRowLinkFlag = 1 << 1
const eventLiveRevisionFieldNumber = 14
const liveObservedRevisionFieldNumber = 17
const nativeScreenRevisionFieldNumber = 17

export function encodeTerminalHelloPayload(init: { version: number; client?: string; server?: string }): Uint8Array {
  return encodeMessage(HelloSchema, {
    version: uint32Value(init.version),
    client: init.client ?? '',
    server: init.server ?? '',
  })
}

export function decodeTerminalHelloPayload(payload: Uint8Array): { version: number; client: string; server: string } {
  const hello = decodeMessage(HelloSchema, payload)
  return {
    version: hello.version,
    client: hello.client,
    server: hello.server,
  }
}

export function encodeTerminalRequestPayload(id: number, method: string, params: unknown): Uint8Array {
  return encodeMessage(RequestEnvelopeSchema, {
    id: BigInt(id),
    method,
    params: encodeTerminalMethodParams(method, params),
  })
}

/**
 * encodeTerminalRequestEnvelope 只重写 connection-level request id，并保留已经按 method 编码的 params。
 * 它供单一物理 termx protocol transport 的客户端 multiplexer 使用，不能改变 daemon method 或 terminal scope。
 */
export function encodeTerminalRequestEnvelope(request: TerminalRequestEnvelope): Uint8Array {
  return encodeMessage(RequestEnvelopeSchema, {
    id: BigInt(request.id),
    method: request.method,
    params: request.params,
  })
}

export function decodeTerminalRequestPayload(payload: Uint8Array): TerminalRequestEnvelope {
  const request = decodeMessage(RequestEnvelopeSchema, payload)
  return {
    id: Number(request.id),
    method: request.method,
    params: request.params,
  }
}

export function decodeTerminalResponsePayload(payload: Uint8Array): TerminalResponseEnvelope {
  const response = decodeMessage(ResponseEnvelopeSchema, payload)
  return {
    id: Number(response.id),
    result: response.result,
  }
}

/** encodeTerminalResponseEnvelope 把物理 response id 恢复成调用方虚拟会话的 request id。 */
export function encodeTerminalResponseEnvelope(response: TerminalResponseEnvelope): Uint8Array {
  return encodeMessage(ResponseEnvelopeSchema, {
    id: BigInt(response.id),
    result: response.result,
  })
}

export function encodeTerminalResponsePayload(id: number, method: string, result: unknown): Uint8Array {
  return encodeMessage(ResponseEnvelopeSchema, {
    id: BigInt(id),
    result: encodeTerminalMethodResult(method, result),
  })
}

export function decodeTerminalErrorPayload(payload: Uint8Array): TerminalErrorEnvelope {
  const error = decodeMessage(ErrorEnvelopeSchema, payload)
  return {
    id: Number(error.id),
    code: error.error?.code ?? 0,
    message: error.error?.message || 'termx protocol error',
  }
}

export function encodeTerminalErrorPayload(id: number, code: number, message: string): Uint8Array {
  return encodeMessage(ErrorEnvelopeSchema, {
    id: BigInt(id),
    error: {
      code,
      message,
    },
  })
}

/** encodeTerminalErrorEnvelope 保留 daemon 错误分类，只重写虚拟会话 request id。 */
export function encodeTerminalErrorEnvelope(error: TerminalErrorEnvelope): Uint8Array {
  return encodeTerminalErrorPayload(error.id, error.code, error.message)
}

export function encodeTerminalMethodParams(method: string, params: unknown): Uint8Array {
  const record = asRecord(params)
  switch (method) {
    case 'list':
      return encodeMessage(EmptySchema, {})
    case 'attach':
      return encodeMessage(AttachParamsSchema, {
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
        mode: stringField(record, 'mode'),
        resizePolicy: stringField(record, 'resize_policy', 'resizePolicy'),
        surfaceId: stringField(record, 'surface_id', 'surfaceId'),
        viewId: stringField(record, 'view_id', 'viewId'),
      })
    case 'ensure_resize':
      return encodeMessage(EnsureResizeParamsSchema, {
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
        channel: uint32Value(field(record, 'channel')),
        cols: uint32Value(field(record, 'cols')),
        rows: uint32Value(field(record, 'rows')),
        resizePolicy: stringField(record, 'resize_policy', 'resizePolicy'),
        surfaceId: stringField(record, 'surface_id', 'surfaceId'),
        viewId: stringField(record, 'view_id', 'viewId'),
      })
    case 'detach':
      return encodeMessage(EnsureResizeParamsSchema, {
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
        channel: uint32Value(field(record, 'channel')),
        surfaceId: stringField(record, 'surface_id', 'surfaceId'),
        viewId: stringField(record, 'view_id', 'viewId'),
      })
    case 'live.screen.get':
      return encodeMessage(GetParamsSchema, {
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
      })
    case 'live.invalidation.next':
      return encodeMessageWithUint64Unknown(GetParamsSchema, {
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
      }, liveObservedRevisionFieldNumber, bigintValue(field(record, 'observed_revision', 'observedRevision')))
    case 'file.list':
      return encodeMessage(FileListParamsSchema, { path: stringField(record, 'path'), cursor: stringField(record, 'cursor'), limit: int32Value(field(record, 'limit')) })
    case 'file.stat':
    case 'file.mkdir':
    case 'file.delete':
      return encodeMessage(FilePathParamsSchema, { path: stringField(record, 'path'), recursive: booleanValue(field(record, 'recursive')) })
    case 'file.preview':
      return encodeMessage(FilePreviewParamsSchema, { path: stringField(record, 'path'), maxBytes: bigintValue(field(record, 'max_bytes', 'maxBytes')) })
    case 'file.rename':
      return encodeMessage(FileRenameParamsSchema, { path: stringField(record, 'path'), newPath: stringField(record, 'new_path', 'newPath'), overwrite: booleanValue(field(record, 'overwrite')) })
    case 'file.copy':
    case 'file.move':
      return encodeMessage(FileCopyMoveParamsSchema, { paths: stringArray(field(record, 'paths')), targetDir: stringField(record, 'target_dir', 'targetDir'), overwrite: booleanValue(field(record, 'overwrite')) })
    case 'file.download.open':
      return encodeMessage(FileDownloadOpenParamsSchema, { path: stringField(record, 'path'), offset: bigintValue(field(record, 'offset')), expectedSize: bigintValue(field(record, 'expected_size', 'expectedSize')), expectedModifiedAtUnixNano: bigintValue(field(record, 'expected_modified_at_unix_nano', 'expectedModifiedAtUnixNano')) })
    case 'file.upload.open':
      return encodeMessage(FileUploadOpenParamsSchema, { path: stringField(record, 'path'), size: bigintValue(field(record, 'size')), overwrite: booleanValue(field(record, 'overwrite')), resumeTransferId: stringField(record, 'resume_transfer_id', 'resumeTransferId') })
    case 'file.transfer.cancel':
      return encodeMessage(FileTransferCancelParamsSchema, { transferId: stringField(record, 'transfer_id', 'transferId') })
    default:
      return encodeMessage(EmptySchema, {})
  }
}

export function decodeTerminalMethodParams(method: string, payload: Uint8Array): unknown {
  switch (method) {
    case 'list':
      decodeMessage(EmptySchema, payload)
      return {}
    case 'attach': {
      const params = decodeMessage(AttachParamsSchema, payload)
      return cleanRecord({
        terminal_id: params.terminalId,
        mode: params.mode,
        resize_policy: params.resizePolicy,
        surface_id: params.surfaceId,
        view_id: params.viewId,
      })
    }
    case 'ensure_resize': {
      const params = decodeMessage(EnsureResizeParamsSchema, payload)
      return cleanRecord({
        terminal_id: params.terminalId,
        channel: params.channel,
        cols: params.cols,
        rows: params.rows,
        resize_policy: params.resizePolicy,
        surface_id: params.surfaceId,
        view_id: params.viewId,
      })
    }
    case 'detach': {
      const params = decodeMessage(EnsureResizeParamsSchema, payload)
      return cleanRecord({
        terminal_id: params.terminalId,
        channel: params.channel,
        surface_id: params.surfaceId,
        view_id: params.viewId,
      })
    }
    case 'live.screen.get': {
      const params = decodeMessage(GetParamsSchema, payload)
      return cleanRecord({
        terminal_id: params.terminalId,
      })
    }
    case 'live.invalidation.next': {
      const params = decodeMessage(GetParamsSchema, payload)
      return cleanRecord({
        terminal_id: params.terminalId,
        observed_revision: uint64UnknownField(params, liveObservedRevisionFieldNumber),
      })
    }
    case 'file.list': { const value = decodeMessage(FileListParamsSchema, payload); return { path: value.path, cursor: value.cursor, limit: value.limit } }
    case 'file.stat': case 'file.mkdir': case 'file.delete': { const value = decodeMessage(FilePathParamsSchema, payload); return { path: value.path, recursive: value.recursive } }
    case 'file.preview': { const value = decodeMessage(FilePreviewParamsSchema, payload); return { path: value.path, max_bytes: Number(value.maxBytes) } }
    case 'file.rename': { const value = decodeMessage(FileRenameParamsSchema, payload); return { path: value.path, new_path: value.newPath, overwrite: value.overwrite } }
    case 'file.copy': case 'file.move': { const value = decodeMessage(FileCopyMoveParamsSchema, payload); return { paths: [...value.paths], target_dir: value.targetDir, overwrite: value.overwrite } }
    case 'file.download.open': { const value = decodeMessage(FileDownloadOpenParamsSchema, payload); return { path: value.path, offset: Number(value.offset), expected_size: Number(value.expectedSize), expected_modified_at_unix_nano: Number(value.expectedModifiedAtUnixNano) } }
    case 'file.upload.open': { const value = decodeMessage(FileUploadOpenParamsSchema, payload); return { path: value.path, size: Number(value.size), overwrite: value.overwrite, resume_transfer_id: value.resumeTransferId } }
    case 'file.transfer.cancel': { const value = decodeMessage(FileTransferCancelParamsSchema, payload); return { transfer_id: value.transferId } }
    default:
      decodeMessage(EmptySchema, payload)
      return {}
  }
}

export function encodeTerminalMethodResult(method: string, result: unknown): Uint8Array {
  const record = asRecord(result)
  switch (method) {
    case 'list':
      return encodeMessage(ListResultSchema, {
        terminals: arrayField(record, 'terminals').map((terminal) => terminalInfoInit(asRecord(terminal))),
      })
    case 'attach':
      return encodeMessage(AttachResultSchema, {
        mode: stringField(record, 'mode'),
        channel: uint32Value(field(record, 'channel')),
        resizeControl: resizeControlInit(field(record, 'resize_control', 'resizeControl')),
      })
    case 'ensure_resize':
      return encodeMessage(EnsureResizeResultSchema, {
        resizeControl: resizeControlInit(field(record, 'resize_control', 'resizeControl')),
        size: sizeInit(field(record, 'size')),
        resized: booleanValue(field(record, 'resized')),
      })
    case 'live.screen.get':
      return encodeMessageWithUint64Unknown(
        SnapshotSchema,
        snapshotInit(record),
        nativeScreenRevisionFieldNumber,
        bigintValue(field(record, 'live_revision', 'liveRevision')),
      )
    case 'live.invalidation.next':
      return encodeMessageWithUint64Unknown(EventSchema, {
        type: uint32Value(field(record, 'type')),
        terminalId: stringField(record, 'terminal_id', 'terminalId'),
        timestampUnixNano: bigintValue(field(record, 'timestamp_unix_nano', 'timestampUnixNano')),
      }, eventLiveRevisionFieldNumber, bigintValue(field(record, 'live_revision', 'liveRevision')))
    case 'file.list':
      return encodeMessage(FileListResultSchema, { path: stringField(record, 'path'), entries: arrayField(record, 'entries').map((entry) => fileEntryInit(asRecord(entry))), nextCursor: stringField(record, 'next_cursor', 'nextCursor') })
    case 'file.stat':
      return encodeMessage(FileEntrySchema, fileEntryInit(record))
    case 'file.preview':
      return encodeMessage(FilePreviewResultSchema, { entry: fileEntryInit(asRecord(field(record, 'entry'))), mimeType: stringField(record, 'mime_type', 'mimeType'), content: bytesValue(field(record, 'content')), truncated: booleanValue(field(record, 'truncated')) })
    case 'file.mkdir': case 'file.rename': case 'file.delete':
      return encodeMessage(FileOperationResultSchema, fileOperationInit(record))
    case 'file.copy': case 'file.move':
      return encodeMessage(FileBatchResultSchema, { results: arrayField(record, 'results').map((item) => fileOperationInit(asRecord(item))) })
    case 'file.download.open': case 'file.upload.open':
      return encodeMessage(FileTransferOpenResultSchema, { transferId: stringField(record, 'transfer_id', 'transferId'), channel: uint32Value(field(record, 'channel')), path: stringField(record, 'path'), offset: bigintValue(field(record, 'offset')), size: bigintValue(field(record, 'size')), modifiedAtUnixNano: bigintValue(field(record, 'modified_at_unix_nano', 'modifiedAtUnixNano')), windowBytes: bigintValue(field(record, 'window_bytes', 'windowBytes')), chunkBytes: int32Value(field(record, 'chunk_bytes', 'chunkBytes')) })
    case 'file.transfer.cancel':
      return encodeMessage(FileTransferCancelResultSchema, { cancelled: booleanValue(field(record, 'cancelled')) })
    default:
      return encodeMessage(EmptySchema, {})
  }
}

export function decodeTerminalMethodResult(method: string, payload: Uint8Array): unknown {
  switch (method) {
    case 'list':
      return { terminals: decodeMessage(ListResultSchema, payload).terminals.map(terminalInfoToAPI) }
    case 'attach':
      return attachResultToAPI(decodeMessage(AttachResultSchema, payload))
    case 'ensure_resize':
      return ensureResizeResultToAPI(decodeMessage(EnsureResizeResultSchema, payload))
    case 'live.screen.get':
      return snapshotToAPI(decodeMessage(SnapshotSchema, payload))
    case 'live.invalidation.next':
      return liveInvalidationToAPI(decodeMessage(EventSchema, payload))
    case 'file.list': { const value = decodeMessage(FileListResultSchema, payload); return { path: value.path, entries: value.entries.map(fileEntryToAPI), next_cursor: value.nextCursor } }
    case 'file.stat': return fileEntryToAPI(decodeMessage(FileEntrySchema, payload))
    case 'file.preview': { const value = decodeMessage(FilePreviewResultSchema, payload); return { entry: fileEntryToAPI(value.entry), mime_type: value.mimeType, content: value.content, truncated: value.truncated } }
    case 'file.mkdir': case 'file.rename': case 'file.delete': return fileOperationToAPI(decodeMessage(FileOperationResultSchema, payload))
    case 'file.copy': case 'file.move': return { results: decodeMessage(FileBatchResultSchema, payload).results.map(fileOperationToAPI) }
    case 'file.download.open': case 'file.upload.open': { const value = decodeMessage(FileTransferOpenResultSchema, payload); return { transfer_id: value.transferId, channel: value.channel, path: value.path, offset: Number(value.offset), size: Number(value.size), modified_at_unix_nano: Number(value.modifiedAtUnixNano), window_bytes: Number(value.windowBytes), chunk_bytes: value.chunkBytes } }
    case 'file.transfer.cancel': return { cancelled: decodeMessage(FileTransferCancelResultSchema, payload).cancelled }
    default:
      if (payload.byteLength > 0) decodeMessage(EmptySchema, payload)
      return {}
  }
}

export function encodeFileTransferDataPayload(value: TerminalFileTransferData): Uint8Array { return encodeMessage(FileTransferDataSchema, { offset: BigInt(value.offset), data: value.data }) }
export function decodeFileTransferDataPayload(payload: Uint8Array): TerminalFileTransferData { const value = decodeMessage(FileTransferDataSchema, payload); return { offset: Number(value.offset), data: value.data } }
export function encodeFileTransferAckPayload(value: TerminalFileTransferAck): Uint8Array { return encodeMessage(FileTransferAckSchema, { offset: BigInt(value.offset), windowBytes: BigInt(value.windowBytes) }) }
export function decodeFileTransferAckPayload(payload: Uint8Array): TerminalFileTransferAck { const value = decodeMessage(FileTransferAckSchema, payload); return { offset: Number(value.offset), windowBytes: Number(value.windowBytes) } }
export function encodeFileTransferFinishPayload(value: TerminalFileTransferFinish): Uint8Array { return encodeMessage(FileTransferFinishSchema, { size: BigInt(value.size), sha256: value.sha256 }) }
export function decodeFileTransferFinishPayload(payload: Uint8Array): TerminalFileTransferFinish { const value = decodeMessage(FileTransferFinishSchema, payload); return { size: Number(value.size), sha256: value.sha256 } }
export function encodeFileTransferResultPayload(value: TerminalFileTransferResult): Uint8Array { return encodeMessage(FileTransferResultSchema, { path: value.path, size: BigInt(value.size), sha256: value.sha256 }) }
export function decodeFileTransferResultPayload(payload: Uint8Array): TerminalFileTransferResult { const value = decodeMessage(FileTransferResultSchema, payload); return { path: value.path, size: Number(value.size), sha256: value.sha256 } }

function fileEntryInit(record: Record<string, unknown>): MessageInitShape<typeof FileEntrySchema> { return { path: stringField(record, 'path'), name: stringField(record, 'name'), type: stringField(record, 'type'), size: bigintValue(field(record, 'size')), mode: uint32Value(field(record, 'mode')), modifiedAtUnixNano: bigintValue(field(record, 'modified_at_unix_nano', 'modifiedAtUnixNano')), linkTarget: stringField(record, 'link_target', 'linkTarget') } }
function fileEntryToAPI(value: FileEntry | undefined): unknown { if (!value) return {}; return { path: value.path, name: value.name, type: value.type, size: Number(value.size), mode: value.mode, modified_at_unix_nano: Number(value.modifiedAtUnixNano), link_target: value.linkTarget } }
function fileOperationInit(record: Record<string, unknown>): MessageInitShape<typeof FileOperationResultSchema> { return { path: stringField(record, 'path'), targetPath: stringField(record, 'target_path', 'targetPath'), success: booleanValue(field(record, 'success')), errorCode: stringField(record, 'error_code', 'errorCode'), errorMessage: stringField(record, 'error_message', 'errorMessage') } }
function fileOperationToAPI(value: MessageShape<typeof FileOperationResultSchema>): unknown { return { path: value.path, target_path: value.targetPath, success: value.success, error_code: value.errorCode, error_message: value.errorMessage } }

function terminalInfoInit(record: Record<string, unknown>): MessageInitShape<typeof TerminalInfoSchema> {
  return {
    id: stringField(record, 'terminal_id', 'terminalId', 'id'),
    name: stringField(record, 'name'),
    command: stringArray(field(record, 'command')),
    tags: stringMap(field(record, 'tags')),
    size: sizeInit(field(record, 'size')),
    state: stringField(record, 'state'),
    cwd: stringField(record, 'cwd'),
    liveCwd: stringField(record, 'live_cwd', 'liveCwd'),
    createdAtUnixNano: bigintValue(field(record, 'created_at_unix_nano', 'createdAtUnixNano')),
    ...(field(record, 'exit_code', 'exitCode') !== undefined
      ? { exitCode: int32Value(field(record, 'exit_code', 'exitCode')) }
      : {}),
    resizeOwnership: resizeOwnershipInit(field(record, 'resize_ownership', 'resizeOwnership')),
    resizeOwnerAttachmentCount: int32Value(field(record, 'resize_owner_attachment_count', 'resizeOwnerAttachmentCount')),
    exitedAtUnixNano: bigintValue(field(record, 'exited_at_unix_nano', 'exitedAtUnixNano')),
  }
}

function terminalInfoToAPI(info: TerminalInfo): unknown {
  return cleanRecord({
    terminal_id: info.id,
    id: info.id,
    name: info.name,
    command: [...info.command],
    tags: { ...info.tags },
    size: sizeToAPI(info.size),
    state: info.state,
    cwd: info.cwd,
    live_cwd: info.liveCwd,
    created_at_unix_nano: Number(info.createdAtUnixNano),
    ...(info.exitCode !== undefined ? { exit_code: info.exitCode } : {}),
    resize_ownership: resizeOwnershipToAPI(info.resizeOwnership),
    resize_owner_attachment_count: info.resizeOwnerAttachmentCount,
    exited_at_unix_nano: Number(info.exitedAtUnixNano),
  })
}

export function encodeGridViewportPayload(viewport: unknown): Uint8Array {
  return encodeMessage(GridViewportSchema, gridViewportInit(asRecord(viewport)))
}

export function decodeGridViewportPayload(payload: Uint8Array): unknown {
  return gridViewportToAPI(decodeMessage(GridViewportSchema, payload))
}

function attachResultToAPI(result: MessageShape<typeof AttachResultSchema>): unknown {
  return cleanRecord({
    mode: result.mode,
    channel: result.channel,
    resize_control: resizeControlToAPI(result.resizeControl),
  })
}

function ensureResizeResultToAPI(result: MessageShape<typeof EnsureResizeResultSchema>): unknown {
  return cleanRecord({
    resize_control: resizeControlToAPI(result.resizeControl),
    size: sizeToAPI(result.size),
    resized: result.resized,
  })
}

function snapshotToAPI(snapshot: Snapshot): unknown {
  const screenRows = rowSetToRows(snapshot.screen)
  const scrollbackRows = rowSetToRows(snapshot.scrollback)
  return cleanRecord({
    terminal_id: snapshot.terminalId,
    size: sizeToAPI(snapshot.size),
    screen_is_alternate: snapshot.screenIsAlternate,
    screen: {
      rows: screenRows,
      alternateScreen: snapshot.screenIsAlternate,
    },
    scrollback: scrollbackRows,
    scrollback_offset: Number(snapshot.scrollbackOffset),
    scrollback_total: Number(snapshot.scrollbackTotal),
    scrollback_logical_total: Number(snapshot.scrollbackLogicalTotal),
    scrollback_has_more: snapshot.scrollbackHasMore,
    cursor: cursorToAPI(snapshot.cursor),
    modes: modesToAPI(snapshot.modes, snapshot.screenIsAlternate),
    timestamp_unix_nano: Number(snapshot.timestampUnixNano),
    live_revision: uint64UnknownField(snapshot, nativeScreenRevisionFieldNumber),
  })
}

function liveInvalidationToAPI(event: Event): unknown {
  return cleanRecord({
    type: event.type,
    terminal_id: event.terminalId,
    timestamp_unix_nano: Number(event.timestampUnixNano),
    live_revision: uint64UnknownField(event, eventLiveRevisionFieldNumber),
  })
}

function gridViewportToAPI(viewport: GridViewport): unknown {
  return cleanRecord({
    terminal_id: viewport.terminalId,
    size: sizeToAPI(viewport.size),
    rows: rowSetToRows(viewport.rows),
    scrollback_offset: Number(viewport.scrollbackOffset),
    scrollback_limit: Number(viewport.scrollbackLimit),
    scrollback_total: Number(viewport.scrollbackTotal),
    scrollback_logical_total: Number(viewport.scrollbackLogicalTotal),
    scrollback_has_more: viewport.scrollbackHasMore,
    loaded_rows: Number(viewport.loadedRows),
    history_generation: Number(viewport.historyGeneration),
    first_row_id: Number(viewport.firstRowId),
    last_row_id: Number(viewport.lastRowId),
    timestamp_unix_nano: Number(viewport.timestampUnixNano),
  })
}

function rowSetToRows(rowSet: RowSet | undefined): Array<Record<string, unknown>> {
  if (!rowSet) return []
  const rows = decodeCompactRowsBlob(rowSet.rowsBlob)
  return rows.map((cells, index) => cleanRecord({
    cells,
    timestamp_unix_nano: rowSet.timestampsUnixNano[index] !== undefined ? Number(rowSet.timestampsUnixNano[index]) : undefined,
    kind: rowSet.rowKinds[index] || undefined,
    wrapped: rowSet.wrapped[index] === true ? true : undefined,
  }))
}

function decodeCompactRowsBlob(blob: Uint8Array): CompactCell[][] {
  if (blob.byteLength === 0) return []
  const dec = new TerminalBinaryDecoder(blob)
  dec.consumeMagic(compactRowsMagic)
  const count = dec.readUvarint()
  if (count > blob.byteLength) {
    throw new Error('invalid terminal compact rows count')
  }
  const rows: CompactCell[][] = []
  for (let index = 0; index < count; index += 1) {
    const size = dec.readUvarint()
    const rowBlob = dec.readBytes(size)
    rows.push(decodeCompactRowBlob(rowBlob))
  }
  dec.assertEOF()
  return rows
}

function decodeCompactRowBlob(blob: Uint8Array): CompactCell[] {
  const dec = new TerminalBinaryDecoder(blob)
  dec.consumeMagic(compactRowMagic)
  const flags = dec.readByte()
  let cells: CompactCell[]
  if ((flags & compactRowFlagRuns) !== 0) {
    const count = dec.readUvarint()
    cells = []
    for (let index = 0; index < count; index += 1) {
      const text = dec.readString()
      const style = dec.readStyle(compactRowStyleFlag)
      const link = dec.readLink(compactRowLinkFlag)
      cells.push(...textToCells(text, style, link))
    }
  } else if ((flags & compactRowFlagCells) !== 0) {
    const count = dec.readUvarint()
    cells = []
    for (let index = 0; index < count; index += 1) {
      const content = dec.readString()
      const width = dec.readUvarint()
      const style = dec.readStyle(compactRowStyleFlag)
      const link = dec.readLink(compactRowLinkFlag)
      cells.push({
        content,
        width,
        style,
        ...link,
      })
    }
  } else {
    cells = textToCells(dec.readString(), emptyStyle)
  }
  dec.assertEOF()
  return cells
}

function textToCells(text: string, style: CompactStyle, link: { linkUrl: string; linkParams: string } = { linkUrl: '', linkParams: '' }): CompactCell[] {
  if (!text) return []
  return Array.from(text).map((content) => ({
    content,
    width: 1,
    style,
    ...link,
  }))
}

function resizeControlToAPI(control: ResizeControl | undefined): unknown {
  if (!control) return undefined
  return cleanRecord({
    can_resize: control.canResize,
    reason: control.reason || 'unknown',
    size_locked: control.sizeLocked ? true : undefined,
    surface_id: control.surfaceId || undefined,
    owner_surface_id: control.ownerSurfaceId || undefined,
    owner_view_id: control.ownerViewId || undefined,
    resize_ownership: resizeOwnershipToAPI(control.resizeOwnership),
  })
}

function resizeOwnershipToAPI(ownership: ResizeOwnership | undefined): unknown {
  if (!ownership) return undefined
  return cleanRecord({
    owner_attachment_id: ownership.ownerAttachmentId,
    owner_surface_id: ownership.ownerSurfaceId,
    owner_view_id: ownership.ownerViewId,
    owner_remote_addr: ownership.ownerRemoteAddr,
    size: sizeToAPI(ownership.size),
    size_locked: ownership.sizeLocked,
    epoch: Number(ownership.epoch),
  })
}

function cursorToAPI(cursor: CursorState | undefined): unknown {
  return {
    row: cursor?.row ?? -1,
    col: cursor?.col ?? -1,
    visible: cursor?.visible ?? true,
    shape: cursorShapeToAPI(cursor?.shape ?? 0),
    blink: cursor?.blink ?? false,
  }
}

function modesToAPI(modes: TerminalModes | undefined, screenIsAlternate: boolean): unknown {
  const mask = modes?.mask ?? 0
  return {
    alternateScreen: screenIsAlternate || (mask & (1 << 0)) !== 0,
    alternateScroll: (mask & (1 << 1)) !== 0,
    mouseTracking: (mask & (1 << 2)) !== 0,
    mouseX10: (mask & (1 << 3)) !== 0,
    mouseNormal: (mask & (1 << 4)) !== 0,
    mouseButtonEvent: (mask & (1 << 5)) !== 0,
    mouseAnyEvent: (mask & (1 << 6)) !== 0,
    mouseSGR: (mask & (1 << 7)) !== 0,
    bracketedPaste: (mask & (1 << 8)) !== 0,
    applicationCursor: (mask & (1 << 9)) !== 0,
    autoWrap: (mask & (1 << 10)) !== 0,
  }
}

function cursorShapeToAPI(shape: number): string {
  switch (shape) {
    case 1:
      return 'underline'
    case 2:
      return 'bar'
    default:
      return 'block'
  }
}

function sizeToAPI(size: { cols: number; rows: number } | undefined): unknown {
  return {
    cols: size?.cols ?? 0,
    rows: size?.rows ?? 0,
  }
}

function snapshotInit(record: Record<string, unknown>): MessageInitShape<typeof SnapshotSchema> {
  const screen = asRecord(field(record, 'screen'))
  const modes = asRecord(field(record, 'modes'))
  const screenIsAlternate = booleanValue(field(record, 'screen_is_alternate', 'screenIsAlternate')) ||
    booleanValue(field(screen, 'alternateScreen', 'alternate_screen')) ||
    booleanValue(field(modes, 'alternateScreen', 'alternate_screen'))
  return {
    terminalId: stringField(record, 'terminal_id', 'terminalId'),
    size: sizeInit(field(record, 'size')),
    screenIsAlternate,
    screen: rowSetInit(field(screen, 'rows')),
    scrollback: rowSetInit(field(record, 'scrollback')),
    scrollbackOffset: BigInt(int32Value(field(record, 'scrollback_offset', 'scrollbackOffset'))),
    scrollbackTotal: BigInt(int32Value(field(record, 'scrollback_total', 'scrollbackTotal'))),
    scrollbackLogicalTotal: BigInt(int32Value(field(record, 'scrollback_logical_total', 'scrollbackLogicalTotal'))),
    scrollbackHasMore: booleanValue(field(record, 'scrollback_has_more', 'scrollbackHasMore')),
    modes: { mask: modesMask(modes, screenIsAlternate) },
    timestampUnixNano: BigInt(0),
  }
}

function gridViewportInit(record: Record<string, unknown>): MessageInitShape<typeof GridViewportSchema> {
  return {
    terminalId: stringField(record, 'terminal_id', 'terminalId'),
    size: sizeInit(field(record, 'size')),
    rows: rowSetInit(field(record, 'rows')),
    scrollbackOffset: BigInt(int32Value(field(record, 'scrollback_offset', 'scrollbackOffset'))),
    scrollbackLimit: BigInt(int32Value(field(record, 'scrollback_limit', 'scrollbackLimit'))),
    scrollbackTotal: BigInt(int32Value(field(record, 'scrollback_total', 'scrollbackTotal'))),
    scrollbackLogicalTotal: BigInt(int32Value(field(record, 'scrollback_logical_total', 'scrollbackLogicalTotal'))),
    scrollbackHasMore: booleanValue(field(record, 'scrollback_has_more', 'scrollbackHasMore')),
    loadedRows: BigInt(int32Value(field(record, 'loaded_rows', 'loadedRows'))),
    historyGeneration: BigInt(int32Value(field(record, 'history_generation', 'historyGeneration'))),
    firstRowId: BigInt(int32Value(field(record, 'first_row_id', 'firstRowId'))),
    lastRowId: BigInt(int32Value(field(record, 'last_row_id', 'lastRowId'))),
    timestampUnixNano: BigInt(0),
  }
}

function modesMask(modes: Record<string, unknown>, screenIsAlternate: boolean): number {
  let mask = screenIsAlternate ? 1 << 0 : 0
  if (booleanValue(field(modes, 'alternateScroll', 'alternate_scroll'))) mask |= 1 << 1
  if (booleanValue(field(modes, 'mouseTracking', 'mouse_tracking'))) mask |= 1 << 2
  if (booleanValue(field(modes, 'mouseX10', 'mouse_x10'))) mask |= 1 << 3
  if (booleanValue(field(modes, 'mouseNormal', 'mouse_normal'))) mask |= 1 << 4
  if (booleanValue(field(modes, 'mouseButtonEvent', 'mouse_button_event'))) mask |= 1 << 5
  if (booleanValue(field(modes, 'mouseAnyEvent', 'mouse_any_event'))) mask |= 1 << 6
  if (booleanValue(field(modes, 'mouseSGR', 'mouse_sgr'))) mask |= 1 << 7
  if (booleanValue(field(modes, 'bracketedPaste', 'bracketed_paste'))) mask |= 1 << 8
  if (booleanValue(field(modes, 'applicationCursor', 'application_cursor'))) mask |= 1 << 9
  if (booleanValue(field(modes, 'autoWrap', 'auto_wrap'))) mask |= 1 << 10
  return mask
}

function rowSetInit(value: unknown): MessageInitShape<typeof SnapshotSchema>['screen'] {
  const rows = Array.isArray(value) ? value : []
  const out: MessageInitShape<typeof SnapshotSchema>['screen'] = {
    rowsBlob: encodeCompactRowsBlob(rowSetRows(rows)),
  }
  const timestamps = rows.map((row) => BigInt(int32Value(field(asRecord(row), 'timestamp_unix_nano', 'timestampUnixNano'))))
  const rowKinds = rows.map((row) => stringField(asRecord(row), 'kind'))
  const wrapped = rows.map((row) => booleanValue(field(asRecord(row), 'wrapped')))
  if (timestamps.some((timestamp) => timestamp !== BigInt(0))) out.timestampsUnixNano = timestamps
  if (rowKinds.some(Boolean)) out.rowKinds = rowKinds
  if (wrapped.some(Boolean)) out.wrapped = wrapped
  return out
}

function resizeControlInit(value: unknown): MessageInitShape<typeof EnsureResizeResultSchema>['resizeControl'] {
  const record = asRecord(value)
  if (Object.keys(record).length === 0) return undefined
  return {
    canResize: booleanValue(field(record, 'can_resize', 'canResize')),
    reason: stringField(record, 'reason'),
    sizeLocked: booleanValue(field(record, 'size_locked', 'sizeLocked')),
    surfaceId: stringField(record, 'surface_id', 'surfaceId'),
    ownerSurfaceId: stringField(record, 'owner_surface_id', 'ownerSurfaceId'),
    ownerViewId: stringField(record, 'owner_view_id', 'ownerViewId'),
  }
}

function encodeCompactRowsBlob(rows: CompactCell[][]): Uint8Array {
  if (rows.length === 0) return new Uint8Array()
  const enc = new TerminalBinaryEncoder()
  enc.appendBytes(asciiBytes(compactRowsMagic))
  enc.appendUvarint(rows.length)
  for (const row of rows) {
    const rowBlob = encodeCompactRowBlob(row)
    enc.appendUvarint(rowBlob.byteLength)
    enc.appendBytes(rowBlob)
  }
  return enc.bytes()
}

function encodeCompactRowBlob(cells: CompactCell[]): Uint8Array {
  const enc = new TerminalBinaryEncoder()
  enc.appendBytes(asciiBytes(compactRowMagic))
  if (cells.length === 0) {
    enc.appendByte(0)
    enc.appendString('')
    return enc.bytes()
  }
  enc.appendByte(compactRowFlagCells)
  enc.appendUvarint(cells.length)
  for (const cell of cells) {
    enc.appendString(cell.content)
    enc.appendUvarint(cell.width)
    enc.appendStyle(cell.style, compactRowStyleFlag)
    enc.appendLink(cell.linkUrl, cell.linkParams, compactRowLinkFlag)
  }
  return enc.bytes()
}

function rowSetRows(value: unknown): CompactCell[][] {
  const rows = Array.isArray(value) ? value : []
  return rows.map((row) => rowCells(row))
}

function rowCells(value: unknown): CompactCell[] {
  if (Array.isArray(value)) {
    return value.map(cellFrom)
  }
  const record = asRecord(value)
  const cells = field(record, 'cells')
  return Array.isArray(cells) ? cells.map(cellFrom) : []
}

function cellFrom(value: unknown): CompactCell {
  const record = asRecord(value)
  const content = stringField(record, 'r', 'content')
  const width = uint32Value(field(record, 'w', 'width')) || (content ? 1 : 0)
  return {
    content,
    width,
    style: styleFrom(field(record, 's', 'style')),
    linkUrl: stringField(record, 'link_url', 'linkUrl'),
    linkParams: stringField(record, 'link_params', 'linkParams'),
  }
}

function styleFrom(value: unknown): CompactStyle {
  const record = asRecord(value)
  if (Object.keys(record).length === 0) return { ...emptyStyle }
  return {
    fg: stringField(record, 'fg'),
    bg: stringField(record, 'bg'),
    bold: booleanValue(field(record, 'b', 'bold')),
    italic: booleanValue(field(record, 'i', 'italic')),
    underline: booleanValue(field(record, 'u', 'underline')),
    blink: booleanValue(field(record, 'k', 'blink')),
    reverse: booleanValue(field(record, 'rv', 'reverse')),
    strikethrough: booleanValue(field(record, 'st', 'strikethrough')),
    linkUrl: stringField(record, 'link_url', 'linkUrl'),
    linkParams: stringField(record, 'link_params', 'linkParams'),
  }
}

function asciiBytes(value: string): Uint8Array {
  const out = new Uint8Array(value.length)
  for (let index = 0; index < value.length; index += 1) {
    out[index] = value.charCodeAt(index)
  }
  return out
}

function sizeInit(value: unknown): { cols: number; rows: number } | undefined {
  const record = asRecord(value)
  if (Object.keys(record).length === 0) return undefined
  return {
    cols: uint32Value(field(record, 'cols')),
    rows: uint32Value(field(record, 'rows')),
  }
}

function resizeOwnershipInit(value: unknown): MessageInitShape<typeof TerminalInfoSchema>['resizeOwnership'] {
  const record = asRecord(value)
  if (Object.keys(record).length === 0) return undefined
  return {
    ownerAttachmentId: stringField(record, 'owner_attachment_id', 'ownerAttachmentId'),
    ownerSurfaceId: stringField(record, 'owner_surface_id', 'ownerSurfaceId'),
    ownerViewId: stringField(record, 'owner_view_id', 'ownerViewId'),
    ownerRemoteAddr: stringField(record, 'owner_remote_addr', 'ownerRemoteAddr'),
    size: sizeInit(field(record, 'size')),
    sizeLocked: booleanValue(field(record, 'size_locked', 'sizeLocked')),
    epoch: bigintValue(field(record, 'epoch')),
  }
}

function arrayField(record: Record<string, unknown>, ...names: string[]): unknown[] {
  const value = field(record, ...names)
  return Array.isArray(value) ? value : []
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function stringMap(value: unknown): Record<string, string> {
  const record = asRecord(value)
  return Object.fromEntries(Object.entries(record).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
}

function bytesValue(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  return new Uint8Array()
}

function bigintValue(value: unknown): bigint {
  if (typeof value === 'bigint') return value
  if (typeof value === 'number' && Number.isFinite(value)) return BigInt(Math.trunc(value))
  if (typeof value === 'string' && /^-?[0-9]+$/.test(value)) return BigInt(value)
  return BigInt(0)
}

function encodeMessage<T extends DescMessage>(schema: T, init: MessageInitShape<T>): Uint8Array {
  return toBinary(schema, create(schema, init))
}

function encodeMessageWithUint64Unknown<T extends DescMessage>(
  schema: T,
  init: MessageInitShape<T>,
  fieldNumber: number,
  value: bigint,
): Uint8Array {
  const message = create(schema, init)
  if (value > BigInt(0)) {
    message.$unknown = [
      ...(message.$unknown ?? []),
      { no: fieldNumber, wireType: WireType.Varint, data: encodeUvarint(value) },
    ]
  }
  return toBinary(schema, message)
}

function decodeMessage<T extends DescMessage>(schema: T, payload: Uint8Array): MessageShape<T> {
  return fromBinary(schema, payload)
}

function uint64UnknownField(message: { $unknown?: UnknownField[] | undefined }, fieldNumber: number): number {
  const field = message.$unknown?.find((candidate) => candidate.no === fieldNumber && candidate.wireType === WireType.Varint)
  if (!field) return 0
  const value = decodeUvarint(field.data)
  if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error(`terminal uint64 field ${fieldNumber} exceeds JavaScript safe range`)
  return Number(value)
}

function encodeUvarint(value: bigint): Uint8Array {
  if (value < BigInt(0)) throw new Error('terminal uint64 field cannot be negative')
  const bytes: number[] = []
  let remaining = value
  while (remaining >= BigInt(0x80)) {
    bytes.push(Number((remaining & BigInt(0x7f)) | BigInt(0x80)))
    remaining >>= BigInt(7)
  }
  bytes.push(Number(remaining))
  return new Uint8Array(bytes)
}

function decodeUvarint(data: Uint8Array): bigint {
  let value = BigInt(0)
  let shift = BigInt(0)
  for (let index = 0; index < data.byteLength && index < 10; index += 1) {
    const byte = data[index] ?? 0
    value |= BigInt(byte & 0x7f) << shift
    if ((byte & 0x80) === 0) {
      if (index !== data.byteLength - 1) throw new Error('terminal uint64 field has trailing bytes')
      return value
    }
    shift += BigInt(7)
  }
  throw new Error('terminal uint64 field is malformed')
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function field(record: Record<string, unknown>, ...names: string[]): unknown {
  for (const name of names) {
    if (Object.prototype.hasOwnProperty.call(record, name)) return record[name]
  }
  return undefined
}

function stringField(record: Record<string, unknown>, ...names: string[]): string {
  const value = field(record, ...names)
  return typeof value === 'string' ? value : ''
}

function uint32Value(value: unknown): number {
  return integerValue(value, 0, 0xffffffff)
}

function int32Value(value: unknown): number {
  return integerValue(value, -0x80000000, 0x7fffffff)
}

function integerValue(value: unknown, min: number, max: number): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return 0
  return Math.min(max, Math.max(min, Math.trunc(value)))
}

function booleanValue(value: unknown): boolean {
  return value === true
}

function cleanRecord<T extends Record<string, unknown>>(record: T): T {
  for (const key of Object.keys(record)) {
    if (record[key] === undefined || record[key] === '') {
      delete record[key]
    }
  }
  return record
}

class TerminalBinaryDecoder {
  private offset = 0
  private readonly textDecoder = new TextDecoder()

  constructor(private readonly data: Uint8Array) {}

  consumeMagic(magic: string): void {
    this.ensure(magic.length)
    if (this.textDecoder.decode(this.data.slice(this.offset, this.offset + magic.length)) !== magic) {
      throw new Error(`invalid terminal binary magic ${magic}`)
    }
    this.offset += magic.length
  }

  readByte(): number {
    this.ensure(1)
    const value = this.data[this.offset] ?? 0
    this.offset += 1
    return value
  }

  readUvarint(): number {
    let value = 0
    let shift = 0
    for (let index = 0; index < 10; index += 1) {
      const byte = this.readByte()
      if (byte < 0x80) {
        value += byte * (2 ** shift)
        return value
      }
      value += (byte & 0x7f) * (2 ** shift)
      shift += 7
    }
    throw new Error('invalid terminal binary varint')
  }

  readBytes(length: number): Uint8Array {
    this.ensure(length)
    const value = this.data.slice(this.offset, this.offset + length)
    this.offset += length
    return value
  }

  readString(): string {
    return this.textDecoder.decode(this.readBytes(this.readUvarint()))
  }

  readStyle(styleFlag: number): CompactStyle {
    const flags = this.readByte()
    if ((flags & styleFlag) === 0) return emptyStyle
    const fg = this.readString()
    const bg = this.readString()
    const mask = this.readByte()
    return {
      fg,
      bg,
      bold: (mask & (1 << 0)) !== 0,
      italic: (mask & (1 << 1)) !== 0,
      underline: (mask & (1 << 2)) !== 0,
      blink: (mask & (1 << 3)) !== 0,
      reverse: (mask & (1 << 4)) !== 0,
      strikethrough: (mask & (1 << 5)) !== 0,
      linkUrl: '',
      linkParams: '',
    }
  }

  readLink(linkFlag: number): { linkUrl: string; linkParams: string } {
    const flags = this.readByte()
    if ((flags & linkFlag) === 0) return { linkUrl: '', linkParams: '' }
    return {
      linkUrl: this.readString(),
      linkParams: this.readString(),
    }
  }

  assertEOF(): void {
    if (this.offset !== this.data.byteLength) {
      throw new Error('terminal binary payload has trailing bytes')
    }
  }

  private ensure(length: number): void {
    if (length < 0 || this.data.byteLength - this.offset < length) {
      throw new Error('unexpected EOF')
    }
  }
}

class TerminalBinaryEncoder {
  private chunks: number[] = []
  private readonly textEncoder = new TextEncoder()

  appendByte(value: number): void {
    this.chunks.push(value & 0xff)
  }

  appendBytes(value: Uint8Array): void {
    for (const byte of value) this.chunks.push(byte)
  }

  appendUvarint(value: number): void {
    let current = Math.max(0, Math.trunc(value))
    while (current >= 0x80) {
      this.appendByte((current & 0x7f) | 0x80)
      current = Math.floor(current / 0x80)
    }
    this.appendByte(current)
  }

  appendString(value: string): void {
    const bytes = this.textEncoder.encode(value)
    this.appendUvarint(bytes.byteLength)
    this.appendBytes(bytes)
  }

  appendStyle(style: CompactStyle, styleFlag: number): void {
    if (isEmptyStyle(style)) {
      this.appendByte(0)
      return
    }
    this.appendByte(styleFlag)
    this.appendString(style.fg)
    this.appendString(style.bg)
    let mask = 0
    if (style.bold) mask |= 1 << 0
    if (style.italic) mask |= 1 << 1
    if (style.underline) mask |= 1 << 2
    if (style.blink) mask |= 1 << 3
    if (style.reverse) mask |= 1 << 4
    if (style.strikethrough) mask |= 1 << 5
    this.appendByte(mask)
  }

  appendLink(linkUrl: string, linkParams: string, linkFlag: number): void {
    if (!linkUrl && !linkParams) {
      this.appendByte(0)
      return
    }
    this.appendByte(linkFlag)
    this.appendString(linkUrl)
    this.appendString(linkParams)
  }

  bytes(): Uint8Array {
    return new Uint8Array(this.chunks)
  }
}

function isEmptyStyle(style: CompactStyle): boolean {
  return !style.fg &&
    !style.bg &&
    !style.bold &&
    !style.italic &&
    !style.underline &&
    !style.blink &&
    !style.reverse &&
    !style.strikethrough &&
    !style.linkUrl &&
    !style.linkParams
}
