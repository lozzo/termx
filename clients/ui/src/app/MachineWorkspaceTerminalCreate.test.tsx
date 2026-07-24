import { create } from '@bufbuild/protobuf'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { forwardRef, useImperativeHandle } from 'react'
import { AcknowledgeResultSchema } from '../generated/apipb/application_pb'
import {
  TerminalCreateResultSchema,
  TerminalDefaultsResultSchema,
  TerminalDefaultsSchema,
  TerminalInfoSchema,
  TerminalRefSchema,
} from '../generated/apipb/terminal_pb'
import { muxviaI18n } from '../i18n'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { MachineWorkspace } from './MachineWorkspace'

vi.mock('../terminal/Terminal', () => ({
  Terminal: forwardRef(function MockTerminal(_props: unknown, ref) {
    useImperativeHandle(ref, () => ({
      sendInput: () => {},
      sendResize: () => {},
      requestResizeOwner: async () => ({ canResize: true, reason: 'owner' }),
      releaseResizeOwner: async () => ({ canResize: false, reason: 'follower' }),
      focus: () => {},
      blur: () => {},
      fit: () => {},
      reattach: () => {},
      selectAll: () => {},
      selectVisible: () => {},
      getSelection: () => '',
      hasSelection: () => false,
      clearSelection: () => {},
      pasteText: () => {},
      getCursorInfo: () => null,
      adjustInputPosition: () => {},
      getBufferType: () => 'normal',
      updateOptions: () => {},
    }))
    return <div data-testid="mock-terminal" />
  }),
}))

describe('MachineWorkspace terminal creation', () => {
  beforeEach(async () => { await muxviaI18n.changeLanguage('en') })
  afterEach(cleanup)

  it('prefills daemon defaults and submits a complete generated Proto create command', async () => {
    const machine = { machineId: 'studio', name: 'Studio', state: 'online' as const }
    const session = new MockProtoSession('studio', (command) => {
      if (command.command.case === 'terminalDefaults') {
        return protoResult('terminalDefaults', create(TerminalDefaultsResultSchema, {
          defaults: create(TerminalDefaultsSchema, { defaultCommand: ['/bin/fish'], defaultCwd: '/home/ada' }),
        }))
      }
      if (command.command.case === 'terminalCreate') {
        return protoResult('terminalCreate', create(TerminalCreateResultSchema, {
          terminal: create(TerminalInfoSchema, {
            ref: create(TerminalRefSchema, { endpointId: 'studio', terminalId: 'term-created' }),
          }),
        }))
      }
      return protoResult('acknowledge', create(AcknowledgeResultSchema))
    })
    const listTerminals = vi.fn(async () => [])

    render(<MachineWorkspace
      api={{
        getStatus: vi.fn(async () => ({ machine, localWeb: { httpUrl: '', rtcOfferUrl: '' } })),
        listTerminals,
      }}
      connector={{ connect: vi.fn(async () => session) }}
      initialMachine={machine}
    />)

    await userEvent.click(await screen.findByRole('button', { name: 'Create terminal' }))
    const sheet = await screen.findByTestId('muxvia-terminal-editor-sheet')
    await waitFor(() => expect((within(sheet).getByLabelText('Command') as HTMLInputElement).value).toBe('/bin/fish'))
    expect((within(sheet).getByLabelText('Working directory') as HTMLInputElement).value).toBe('/home/ada')

    await userEvent.click(within(sheet).getByRole('button', { name: 'Add variable' }))
    await userEvent.type(within(sheet).getByLabelText('Key'), 'MODE')
    await userEvent.type(within(sheet).getByLabelText('Value'), 'mobile')
    await userEvent.click(within(sheet).getByRole('button', { name: 'Create terminal' }))

    await waitFor(() => expect(session.commands.some((command) => command.command.case === 'terminalCreate')).toBe(true))
    const createCommand = session.commands.find((command) => command.command.case === 'terminalCreate')
    expect(createCommand?.command.value).toMatchObject({
      terminal: {
        terminalId: expect.stringMatching(/^term-/),
        command: ['/bin/fish'],
        cwd: '/home/ada',
        env: ['MODE=mobile'],
        size: { cols: 80, rows: 24 },
      },
    })
  })

  it('keeps files, resize, and terminal tools visible in the terminal header', async () => {
    const machine = { machineId: 'studio', name: 'Studio', state: 'online' as const }
    const terminal = {
      terminalId: 'term-shell', machineId: 'studio', title: 'Shell', state: 'running' as const,
      command: '/bin/zsh', cols: 80, rows: 24,
    }
    const session = new MockProtoSession('studio', () => protoResult('acknowledge', create(AcknowledgeResultSchema)))

    render(<MachineWorkspace
      api={{
        getStatus: vi.fn(async () => ({ machine, localWeb: { httpUrl: '', rtcOfferUrl: '' } })),
        listTerminals: vi.fn(async () => [terminal]),
      }}
      connector={{ connect: vi.fn(async () => session) }}
      initialMachine={machine}
    />)

    await userEvent.click(await screen.findByRole('button', { name: 'Open Shell' }))
    const header = await screen.findByTestId('muxvia-terminal-header')
    expect(within(header).getByRole('button', { name: 'Open files' })).toBeTruthy()
    expect(within(header).getByRole('button', { name: 'Control resize' })).toBeTruthy()
    expect(within(header).getByRole('button', { name: 'Terminal tools' })).toBeTruthy()
    expect(within(header).getByRole('button', { name: 'Open terminal menu' })).toBeTruthy()
  })
})
