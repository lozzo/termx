import { describe, expect, it } from 'vitest'
import {
  decodeTerminalMethodParams,
  decodeTerminalMethodResult,
  encodeTerminalMethodParams,
  encodeTerminalMethodResult,
} from './terminalWireProtocol'

describe('terminal management wire protocol', () => {
  it('round trips create params and result through the core-v2 protobuf contract', () => {
    const params = {
      command: ['/bin/zsh', '-l'],
      id: 'relay-test',
      name: 'relay-test',
      tags: { 'termx.size_lock': 'off', cwd: '/Users/lozzow' },
      size: { cols: 120, rows: 36 },
      dir: '/Users/lozzow',
      env: ['TERM=xterm-256color'],
      scrollback_size: 1000,
      scrollback_max_bytes: 1048576,
      scrollback_max_age_seconds: 3600,
    }

    expect(decodeTerminalMethodParams('create', encodeTerminalMethodParams('create', params))).toEqual(params)
    expect(decodeTerminalMethodResult('create', encodeTerminalMethodResult('create', {
      terminal_id: 'relay-test',
      state: 'running',
    }))).toEqual({ terminal_id: 'relay-test', state: 'running' })
  })

  it('round trips the remaining terminal management methods without an empty-payload fallback', () => {
    expect(decodeTerminalMethodParams('set_metadata', encodeTerminalMethodParams('set_metadata', {
      terminal_id: 'relay-test',
      name: 'renamed',
      tags: { environment: 'staging' },
    }))).toEqual({ terminal_id: 'relay-test', name: 'renamed', tags: { environment: 'staging' } })

    for (const method of ['get', 'restart', 'remove'] as const) {
      expect(decodeTerminalMethodParams(method, encodeTerminalMethodParams(method, {
        terminal_id: 'relay-test',
      }))).toEqual({ terminal_id: 'relay-test' })
    }

    const terminal = {
      terminal_id: 'relay-test',
      name: 'relay-test',
      command: ['/bin/zsh'],
      tags: {},
      size: { cols: 120, rows: 36 },
      state: 'running',
      cwd: '/Users/lozzow',
      live_cwd: '/Users/lozzow',
      created_at_unix_nano: 123,
      resize_ownership: { policy: 'collaborative', owner_surface_id: '', owner_view_id: '' },
      resize_owner_attachment_count: 0,
      exited_at_unix_nano: 0,
    }
    expect(decodeTerminalMethodResult('get', encodeTerminalMethodResult('get', terminal))).toEqual(expect.objectContaining({
      terminal_id: 'relay-test',
      id: 'relay-test',
      state: 'running',
      cwd: '/Users/lozzow',
      live_cwd: '/Users/lozzow',
    }))
  })
})
