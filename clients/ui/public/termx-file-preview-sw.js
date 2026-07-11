const previewPathPrefix = '/__termx_file_preview__/'
const byte = 1024 * 1024
const defaultRangeChunkSize = 2 * byte
const fallbackReadAheadBytes = 32 * byte
const maxReadAheadBytes = 128 * byte
const targetReadAheadSeconds = 30
const fallbackCacheBytes = 256 * byte
const maxCacheBytes = 512 * byte
const maxCacheSeconds = 120
const rangeRequestTimeoutMs = 120000
const pendingRequests = new Map()
const controllers = new Map()
const previewCaches = new Map()

self.addEventListener('install', (event) => {
  self.skipWaiting()
  event.waitUntil(Promise.resolve())
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('message', (event) => {
  const data = event.data || {}
  if (data.type === 'termx-preview-response') {
    const pending = pendingRequests.get(data.requestId)
    if (!pending) return
    pendingRequests.delete(data.requestId)
    pending.resolve(data)
    return
  }
  if (data.type === 'termx-preview-error') {
    const pending = pendingRequests.get(data.requestId)
    if (!pending) return
    pendingRequests.delete(data.requestId)
    pending.reject(new Error(data.message || 'file preview range failed'))
    return
  }
  if (data.type === 'termx-preview-configure' && typeof data.token === 'string') {
    const cache = previewCache(data.token, numberParam(data.size), data.mimeType || 'application/octet-stream')
    cache.duration = positiveNumber(data.duration) || cache.duration
    return
  }
  if (data.type === 'termx-preview-release' && typeof data.token === 'string') {
    previewCaches.delete(data.token)
    controllers.delete(data.token)
  }
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (url.origin !== self.location.origin || !url.pathname.startsWith(previewPathPrefix)) return
  if (event.request.method !== 'GET' && event.request.method !== 'HEAD') return
  event.respondWith(handlePreviewRangeRequest(event.request, url, event.clientId, event))
})

async function handlePreviewRangeRequest(request, url, clientId, event) {
  const token = url.pathname.slice(previewPathPrefix.length)
  const controller = await previewController(token, clientId)
  if (!controller) return new Response('file preview is not connected', { status: 404 })

  const size = numberParam(url.searchParams.get('size'))
  const mimeType = url.searchParams.get('mime') || 'application/octet-stream'
  const cache = previewCache(token, size, mimeType)
  const range = parseRange(request.headers.get('range'), size)
  if (!range) {
    return new Response(null, {
      status: 416,
      headers: {
        'content-range': `bytes */${size}`,
        'accept-ranges': 'bytes',
      },
    })
  }
  if (request.method === 'HEAD') {
    return new Response(null, {
      status: range.partial ? 206 : 200,
      headers: rangeHeaders(range, size, mimeType),
    })
  }

  await ensureCachedRange(cache, controller, token, range.start, range.end + 1)
  const blob = cachedBlob(cache, range.start, range.end + 1, mimeType)
  if (!blob) return new Response('file preview cache miss', { status: 502 })

  const prefetchEnd = Math.min(cache.size, range.start + readAheadBytes(cache))
  event.waitUntil(prefetchReadAhead(cache, controller, token, range.start, prefetchEnd))
  evictCache(cache, range.start, Math.max(range.end + 1, prefetchEnd))
  return new Response(blob, {
    status: range.partial ? 206 : 200,
    headers: rangeHeaders(range, size, mimeType, blob.size),
  })
}

async function previewController(token, clientId) {
  const known = controllers.get(token)
  if (known) return known
  if (clientId) {
    const client = await self.clients.get(clientId)
    if (client) {
      controllers.set(token, client)
      return client
    }
  }
  const clients = await self.clients.matchAll({ includeUncontrolled: true, type: 'window' })
  const controller = clients[0]
  if (controller) controllers.set(token, controller)
  return controller
}

function previewCache(token, size, mimeType) {
  const known = previewCaches.get(token)
  if (known) {
    known.size = Math.max(known.size, size)
    known.mimeType = mimeType || known.mimeType
    known.lastAccess = Date.now()
    return known
  }
  const cache = {
    token,
    size,
    mimeType,
    duration: 0,
    bytes: 0,
    lastAccess: Date.now(),
    segments: [],
    inflight: new Map(),
  }
  previewCaches.set(token, cache)
  return cache
}

async function ensureCachedRange(cache, client, token, start, endExclusive) {
  if (cache.size <= 0 || endExclusive <= start) return
  let cursor = alignChunkStart(start)
  const targetEnd = Math.min(cache.size, Math.max(endExclusive, start + 1))
  while (cursor < targetEnd) {
    const chunkStart = cursor
    const chunkEnd = Math.min(cache.size, chunkStart + defaultRangeChunkSize)
    if (!hasCachedCoverage(cache, chunkStart, chunkEnd)) {
      await requestCachedChunk(cache, client, token, chunkStart, chunkEnd - chunkStart)
    }
    cursor = chunkEnd
  }
}

async function requestCachedChunk(cache, client, token, offset, length) {
  if (length <= 0) return
  const key = `${offset}:${length}`
  const known = cache.inflight.get(key)
  if (known) {
    await known
    return
  }
  const request = requestPreviewRange(client, token, offset, length)
    .then((response) => {
      const blob = response.blob instanceof Blob
        ? response.blob
        : new Blob([response.blob || new ArrayBuffer(0)], { type: cache.mimeType })
      const responseOffset = typeof response.offset === 'number' && Number.isFinite(response.offset)
        ? Math.floor(response.offset)
        : offset
      addCachedSegment(cache, responseOffset, blob)
    })
    .finally(() => {
      cache.inflight.delete(key)
    })
  cache.inflight.set(key, request)
  await request
}

function requestPreviewRange(client, token, offset, length) {
  const requestId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      pendingRequests.delete(requestId)
      reject(new Error('file preview range timed out'))
    }, rangeRequestTimeoutMs)
    pendingRequests.set(requestId, {
      resolve(value) {
        clearTimeout(timeout)
        resolve(value)
      },
      reject(error) {
        clearTimeout(timeout)
        reject(error)
      },
    })
    client.postMessage({
      type: 'termx-preview-range-request',
      requestId,
      token,
      offset,
      length,
    })
  })
}

function addCachedSegment(cache, start, blob) {
  if (!blob || blob.size <= 0) return
  const segment = {
    start,
    end: start + blob.size,
    blob,
    lastAccess: Date.now(),
  }
  for (let index = cache.segments.length - 1; index >= 0; index -= 1) {
    const existing = cache.segments[index]
    if (existing.start === segment.start && existing.end === segment.end) {
      cache.bytes -= existing.blob.size
      cache.segments.splice(index, 1)
    }
  }
  cache.segments.push(segment)
  cache.segments.sort((left, right) => left.start - right.start)
  cache.bytes += blob.size
}

function hasCachedCoverage(cache, start, endExclusive) {
  return cache.segments.some((segment) => segment.start <= start && segment.end >= endExclusive)
}

function cachedBlob(cache, start, endExclusive, mimeType) {
  const parts = []
  let cursor = start
  const now = Date.now()
  for (const segment of cache.segments) {
    if (segment.end <= cursor) continue
    if (segment.start > cursor) return null
    const sliceStart = Math.max(0, cursor - segment.start)
    const sliceEnd = Math.min(segment.blob.size, endExclusive - segment.start)
    if (sliceEnd > sliceStart) {
      parts.push(segment.blob.slice(sliceStart, sliceEnd, mimeType))
      segment.lastAccess = now
      cursor = segment.start + sliceEnd
    }
    if (cursor >= endExclusive) break
  }
  if (cursor < endExclusive) return null
  cache.lastAccess = now
  return new Blob(parts, { type: mimeType })
}

async function prefetchReadAhead(cache, client, token, start, endExclusive) {
  try {
    await ensureCachedRange(cache, client, token, start, endExclusive)
    evictCache(cache, start, endExclusive)
  } catch {
    // Playback can continue from the demand-loaded range; the browser will retry later ranges.
  }
}

function evictCache(cache, protectStart, protectEnd) {
  const capacity = cacheCapacity(cache)
  if (cache.bytes <= capacity) return
  const candidates = cache.segments
    .filter((segment) => segment.end <= protectStart || segment.start >= protectEnd)
    .sort((left, right) => left.lastAccess - right.lastAccess)
  for (const segment of candidates) {
    if (cache.bytes <= capacity) return
    const index = cache.segments.indexOf(segment)
    if (index >= 0) {
      cache.segments.splice(index, 1)
      cache.bytes -= segment.blob.size
    }
  }
}

function readAheadBytes(cache) {
  const rate = bytesPerSecond(cache)
  if (!rate) return Math.min(cache.size || fallbackReadAheadBytes, fallbackReadAheadBytes)
  return clamp(Math.floor(rate * targetReadAheadSeconds), defaultRangeChunkSize, Math.min(maxReadAheadBytes, cache.size || maxReadAheadBytes))
}

function cacheCapacity(cache) {
  const rate = bytesPerSecond(cache)
  const timeBound = rate ? Math.floor(rate * maxCacheSeconds) : fallbackCacheBytes
  const fileBound = cache.size > 0 ? cache.size : maxCacheBytes
  return Math.max(defaultRangeChunkSize, Math.min(fileBound, maxCacheBytes, timeBound))
}

function bytesPerSecond(cache) {
  return cache.duration > 0 && cache.size > 0 ? cache.size / cache.duration : 0
}

function alignChunkStart(offset) {
  return Math.max(0, Math.floor(offset / defaultRangeChunkSize) * defaultRangeChunkSize)
}

function parseRange(header, size) {
  if (size < 0) return null
  if (size === 0) return { start: 0, end: 0, length: 0, partial: false }
  if (!header) {
    const end = Math.min(Math.max(0, size - 1), defaultRangeChunkSize - 1)
    return { start: 0, end, length: end + 1, partial: true }
  }
  const match = /^bytes=(\d*)-(\d*)$/.exec(header.trim())
  if (!match) return null
  let start
  let end
  if (!match[1] && match[2]) {
    const suffixLength = Number(match[2])
    if (!Number.isFinite(suffixLength) || suffixLength <= 0) return null
    start = Math.max(0, size - Math.min(Math.floor(suffixLength), defaultRangeChunkSize))
    end = size - 1
  } else {
    start = match[1] ? Number(match[1]) : 0
    end = match[2] ? Number(match[2]) : size - 1
  }
  if (!Number.isFinite(start) || !Number.isFinite(end)) return null
  start = Math.floor(start)
  end = Math.floor(end)
  if (start < 0 || end < start || start >= size) return null
  end = Math.min(end, size - 1, start + defaultRangeChunkSize - 1)
  return { start, end, length: end - start + 1, partial: true }
}

function rangeHeaders(range, size, mimeType, actualLength = range.length) {
  const headers = new Headers()
  headers.set('accept-ranges', 'bytes')
  headers.set('content-type', mimeType)
  headers.set('content-length', String(actualLength))
  if (size > 0) {
    headers.set('content-range', `bytes ${range.start}-${range.start + Math.max(0, actualLength - 1)}/${size}`)
  }
  headers.set('cache-control', 'no-store')
  return headers
}

function numberParam(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? Math.floor(parsed) : 0
}

function positiveNumber(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value))
}
