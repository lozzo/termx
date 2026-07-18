import { describe, expect, it } from 'vitest'
import { verifyRemoteDTLSCertificate } from './browserWasmPlatform'

describe('browser WASM DTLS channel binding', () => {
  it('accepts only the SHA-256 digest of the actual remote certificate', async () => {
    const certificate = new TextEncoder().encode('remote-certificate').buffer
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', certificate))
    const expected = `sha-256:${[...digest].map((byte) => byte.toString(16).padStart(2, '0')).join(':')}`

    await expect(verifyRemoteDTLSCertificate(expected, certificate)).resolves.toBe(expected)
    await expect(verifyRemoteDTLSCertificate(`sha-256:${'00:'.repeat(31)}00`, certificate))
      .rejects.toThrow('does not match remote SDP fingerprint')
  })
})
