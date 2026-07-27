import { Capacitor } from '@capacitor/core'
import { NativeConnection } from './plugins/nativeConnection'

type NativeLogLevel = 'debug' | 'info' | 'warn' | 'error'

const MAX_NATIVE_MESSAGE_CHARS = 16 * 1024
const queue: Array<{ level: NativeLogLevel; tag: string; message: string }> = []
let flushing = false
let dropped = 0

export function installNativeDebugLogCapture(): void {
  if (!Capacitor.isNativePlatform()) return
  ;(globalThis as {
    __anyttyWriteNativeDebugLog?: (level: NativeLogLevel, tag: string, message: string) => void
  }).__anyttyWriteNativeDebugLog = writeNativeDebugLog
  const originalConsole = {
    debug: console.debug.bind(console),
    info: console.info.bind(console),
    log: console.log.bind(console),
    warn: console.warn.bind(console),
    error: console.error.bind(console),
  }

  console.debug = (...args: unknown[]) => {
    originalConsole.debug(...args)
    enqueueNativeDebugLog('debug', 'JSConsole', args)
  }
  console.info = (...args: unknown[]) => {
    originalConsole.info(...args)
    enqueueNativeDebugLog('info', 'JSConsole', args)
  }
  console.log = (...args: unknown[]) => {
    originalConsole.log(...args)
    enqueueNativeDebugLog('info', 'JSConsole', args)
  }
  console.warn = (...args: unknown[]) => {
    originalConsole.warn(...args)
    enqueueNativeDebugLog('warn', 'JSConsole', args)
  }
  console.error = (...args: unknown[]) => {
    originalConsole.error(...args)
    enqueueNativeDebugLog('error', 'JSConsole', args)
  }

  window.addEventListener('error', (event) => {
    enqueueNativeDebugLog('error', 'JSWindowError', [
      event.message,
      { source: event.filename, line: event.lineno, column: event.colno, error: event.error },
    ])
  })
  window.addEventListener('unhandledrejection', (event) => {
    enqueueNativeDebugLog('error', 'JSUnhandledRejection', [event.reason])
  })
  enqueueNativeDebugLog('info', 'JSConsole', ['native debug log capture installed'])
}

export function writeNativeDebugLog(level: NativeLogLevel, tag: string, message: string): void {
  if (!Capacitor.isNativePlatform()) return
  queue.push({ level, tag, message: truncate(message) })
  if (queue.length > 200) {
    queue.splice(0, queue.length - 200)
    dropped += 1
  }
  scheduleFlush()
}

function enqueueNativeDebugLog(level: NativeLogLevel, tag: string, args: unknown[]): void {
  writeNativeDebugLog(level, tag, formatConsoleArgs(args))
}

function scheduleFlush(): void {
  if (flushing) return
  flushing = true
  setTimeout(flushNativeDebugLogs, 0)
}

async function flushNativeDebugLogs(): Promise<void> {
  while (queue.length > 0) {
    const next = queue.shift()
    if (!next) continue
    const message = dropped > 0
      ? `[dropped=${dropped}] ${next.message}`
      : next.message
    dropped = 0
    try {
      await NativeConnection.writeDebugLog({
        level: next.level,
        tag: next.tag,
        message,
      })
    } catch {
      break
    }
  }
  flushing = false
  if (queue.length > 0) scheduleFlush()
}

function formatConsoleArgs(args: unknown[]): string {
  return args.map(formatConsoleArg).join(' ')
}

function formatConsoleArg(value: unknown): string {
  if (typeof value === 'string') return truncate(value)
  if (value instanceof Error) {
    return truncate(`${value.name}: ${value.message}\n${value.stack ?? ''}`)
  }
  try {
    return truncate(JSON.stringify(value))
  } catch {
    return truncate(String(value))
  }
}

function truncate(value: string): string {
  return value.length <= MAX_NATIVE_MESSAGE_CHARS
    ? value
    : `${value.slice(0, MAX_NATIVE_MESSAGE_CHARS)}...<truncated ${value.length - MAX_NATIVE_MESSAGE_CHARS} chars>`
}
