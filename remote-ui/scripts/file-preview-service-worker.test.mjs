import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { Script, createContext } from 'node:vm'
import { describe, expect, it } from 'vitest'

describe('termx file preview service worker', () => {
  it('read-aheads through bounded WebRTC range chunks and reuses cached bytes', async () => {
    const runtime = createPreviewWorkerRuntime()
    const requests = []

    const response = await runtime.fetchRange('bytes=0-1023', (message) => {
      requests.push({
        requestId: message.requestId,
        offset: message.offset,
        length: message.length,
      })
      runtime.respond(message.requestId, message.offset, message.length)
    })
    await runtime.flushBackground((message) => {
      requests.push({
        requestId: message.requestId,
        offset: message.offset,
        length: message.length,
      })
      runtime.respond(message.requestId, message.offset, message.length)
    })

    expect(response.status).toBe(206)
    expect(response.headers.get('content-range')).toBe('bytes 0-1023/104857600')
    expect(response.headers.get('content-length')).toBe('1024')
    expect(requests.map(({ offset, length }) => ({ offset, length }))).toEqual([
      { offset: 0, length: 2 * 1024 * 1024 },
      { offset: 2 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 4 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 6 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 8 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 10 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 12 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 14 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 16 * 1024 * 1024, length: 2 * 1024 * 1024 },
      { offset: 18 * 1024 * 1024, length: 2 * 1024 * 1024 },
    ])

    const requestCount = requests.length
    const cachedResponse = await runtime.fetchRange('bytes=1048576-1049599', (message) => {
      requests.push({
        requestId: message.requestId,
        offset: message.offset,
        length: message.length,
      })
      runtime.respond(message.requestId, message.offset, message.length)
    })

    expect(cachedResponse.status).toBe(206)
    expect(cachedResponse.headers.get('content-range')).toBe('bytes 1048576-1049599/104857600')
    expect(requests).toHaveLength(requestCount)
  })
})

function createPreviewWorkerRuntime() {
  const listeners = new Map()
  const postedMessages = []
  const backgroundTasks = []
  const responseBody = new Uint8Array(2 * 1024 * 1024)
  const controller = {
    postMessage(message) {
      postedMessages.push(message)
    },
  }
  const context = createContext({
    Blob,
    Headers,
    Promise,
    Response,
    URL,
    ArrayBuffer,
    Uint8Array,
    Map,
    Error,
    Math,
    Date,
    Number,
    setTimeout,
    clearTimeout,
    self: {
      location: { origin: 'https://termx.test' },
      skipWaiting() {},
      clients: {
        claim: () => Promise.resolve(),
        get: () => Promise.resolve(controller),
        matchAll: () => Promise.resolve([controller]),
      },
      addEventListener(type, listener) {
        const current = listeners.get(type) ?? []
        current.push(listener)
        listeners.set(type, current)
      },
    },
  })
  new Script(readFileSync(resolve('public/termx-file-preview-sw.js'), 'utf8')).runInContext(context)
  const fetchListener = listeners.get('fetch')?.[0]
  const messageListener = listeners.get('message')?.[0]
  if (!fetchListener || !messageListener) throw new Error('preview worker did not register expected listeners')

  const respond = (requestId, offset, length) => {
    messageListener({
      data: {
        type: 'termx-preview-response',
        requestId,
        offset,
        length,
        totalSize: 100 * 1024 * 1024,
        mimeType: 'video/mp4',
        blob: new Blob([responseBody.slice(0, length)], { type: 'video/mp4' }),
      },
    })
  }

  messageListener({
    data: {
      type: 'termx-preview-configure',
      token: 'preview-test',
      size: 100 * 1024 * 1024,
      mimeType: 'video/mp4',
      duration: 150,
    },
  })

  const drainPostedMessages = async (onRequest) => {
    for (let index = 0; index < 80; index += 1) {
      while (postedMessages.length > 0) onRequest(postedMessages.shift())
      await new Promise((resolveTick) => setTimeout(resolveTick, 0))
      if (postedMessages.length === 0) return
    }
    throw new Error('preview worker still had pending requests')
  }

  return {
    respond,
    async flushBackground(onRequest) {
      await drainPostedMessages(onRequest)
      await Promise.all(backgroundTasks.splice(0))
      await drainPostedMessages(onRequest)
    },
    async fetchRange(range, onRequest) {
      let responsePromise
      fetchListener({
        clientId: 'client-1',
        request: {
          method: 'GET',
          url: 'https://termx.test/__termx_file_preview__/preview-test?size=104857600&mime=video%2Fmp4',
          headers: new Headers({ range }),
        },
        respondWith(promise) {
          responsePromise = promise
        },
        waitUntil(promise) {
          backgroundTasks.push(promise)
        },
      })
      if (!responsePromise) throw new Error('preview worker did not provide a response')
      for (let index = 0; index < 80; index += 1) {
        while (postedMessages.length > 0) onRequest(postedMessages.shift())
        const response = await Promise.race([
          responsePromise.then((value) => ({ done: true, value })),
          new Promise((resolveDelay) => setTimeout(() => resolveDelay({ done: false }), 0)),
        ])
        if (response.done) return response.value
      }
      throw new Error('preview worker fetch did not resolve')
    },
  }
}
