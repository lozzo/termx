import { create } from '@bufbuild/protobuf'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AcknowledgeResultSchema } from '../generated/apipb/application_pb'
import { anyttyI18n } from '../i18n'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { MachineWorkspace } from './MachineWorkspace'

const fileManagerModule = vi.hoisted(() => ({ evaluations: 0 }))

vi.mock('../terminal/Terminal', () => ({ Terminal: () => null }))

vi.mock('../files/FileManager', async () => {
  const { useState } = await vi.importActual<typeof import('react')>('react')
  fileManagerModule.evaluations += 1
  return {
    FileManager({ active }: { active?: boolean }) {
      const [value, setValue] = useState('')
      return (
        <div data-active={String(active)} data-testid="mock-file-manager">
          <input
            aria-label="Lazy file manager state"
            value={value}
            onChange={(event) => setValue(event.currentTarget.value)}
          />
        </div>
      )
    },
  }
})

describe('MachineWorkspace FileManager loading', () => {
  beforeEach(async () => {
    fileManagerModule.evaluations = 0
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('loads on first open, stays mounted while closed, and reuses the loaded module', async () => {
    const machine = { machineId: 'studio', name: 'Studio', state: 'online' as const }
    const session = new MockProtoSession(
      machine.machineId,
      () => protoResult('acknowledge', create(AcknowledgeResultSchema)),
    )

    render(
      <MachineWorkspace
        api={{
          getStatus: vi.fn(async () => ({ machine, localWeb: { httpUrl: '', rtcOfferUrl: '' } })),
          listTerminals: vi.fn(async () => []),
        }}
        connector={{ connect: vi.fn(async () => session) }}
        initialMachine={machine}
      />,
    )

    await screen.findByTestId('anytty-terminal-list-page')
    expect(fileManagerModule.evaluations).toBe(0)
    expect(screen.queryByTestId('anytty-machine-files-overlay')).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: 'Open files' }))
    const stateInput = await screen.findByRole('textbox', { name: 'Lazy file manager state' })
    expect(fileManagerModule.evaluations).toBe(1)
    await userEvent.type(stateInput, '/remembered')

    await userEvent.click(screen.getByRole('button', { name: 'Close files' }))
    await waitFor(() => expect(screen.getByTestId('mock-file-manager').dataset.active).toBe('false'))
    expect(screen.getByRole('textbox', { name: 'Lazy file manager state' })).toBe(stateInput)

    await userEvent.click(screen.getByRole('button', { name: 'Open files' }))
    await waitFor(() => expect(screen.getByTestId('mock-file-manager').dataset.active).toBe('true'))
    expect(screen.getByRole<HTMLInputElement>('textbox', { name: 'Lazy file manager state' }).value).toBe('/remembered')
    expect(fileManagerModule.evaluations).toBe(1)
  }, 15_000)
})
