import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { FileManager, type FileManagerProps } from './FileManager'
import { createMockFileSession } from './test/mockFileSession'

describe('FileManager', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders directory entries and navigates directories through the injected session', async () => {
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => ({
        path,
        parent: path === '/' ? '' : '/',
        total: 1,
        entries: [{ name: path === '/' ? 'tmp' : 'log.txt', type: path === '/' ? 'dir' : 'file', size: 42 }],
      }),
    }, {}, { terminalId: 'terminal-1' })

    render(
      <FileManager
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        initialPath="/"
      />,
    )

    await waitFor(() => expect(screen.getByText('tmp')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').className).toMatch(/\brelative\b/)
    expect(screen.getByTestId('termx-file-manager').className).toMatch(/\bmin-h-0\b/)
    await userEvent.click(screen.getByRole('button', { name: /open tmp/i }))
    await waitFor(() => expect(screen.getByText('log.txt')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('supports hidden files, creating directories, renaming, and delete confirmation through the file api', async () => {
    const entries = [
      { name: '.env', type: 'file', size: 12 },
      { name: 'tmp', type: 'dir', size: 0 },
      { name: 'old.txt', type: 'file', size: 42 },
    ]
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => ({
        path: path || '/',
        parent: '',
        total: entries.length,
        entries,
      }),
      '/files/mkdir': {},
      '/files/rename': {},
      '/files/delete': {},
    }, {}, { terminalId: 'terminal-1' })

    render(
      <FileManager
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        initialPath="/"
      />,
    )

    await waitFor(() => expect(screen.getByText('tmp')).toBeTruthy())
    expect(screen.queryByText('.env')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /show hidden files/i }))
    expect(screen.getByText('.env')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /new directory/i }))
    await userEvent.type(screen.getByLabelText('Directory name'), 'logs')
    await userEvent.click(screen.getByRole('button', { name: /create directory/i }))
    await waitFor(() => expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/mkdir',
      params: { path: '/logs' },
    }))

    await userEvent.click(screen.getByRole('button', { name: /more actions for old.txt/i }))
    await userEvent.click(screen.getByRole('button', { name: /rename/i }))
    await userEvent.clear(screen.getByLabelText('Rename entry'))
    await userEvent.type(screen.getByLabelText('Rename entry'), 'new.txt{Enter}')
    await waitFor(() => expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/rename',
      params: { path: '/old.txt', new_path: '/new.txt' },
    }))

    await userEvent.click(screen.getByRole('button', { name: /more actions for old.txt/i }))
    await userEvent.click(screen.getByRole('button', { name: /delete/i }))
    expect(screen.getByTestId('termx-file-delete-confirm')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    await waitFor(() => expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/delete',
      params: { path: '/old.txt' },
    }))
  })

  it('keeps props interface-based and free of browser/native runtime implementations', () => {
    const propKeys = Object.keys({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session: createMockFileSession(),
    } satisfies FileManagerProps)

    expect(propKeys).not.toContain('webrtcTransport')
    expect(propKeys).not.toContain('rtcPeerConnection')
    expect(propKeys).not.toContain('nativePlugin')
    expect(propKeys).not.toContain('relayCredentials')
  })

  it('allows machine-scoped file manager props without a terminal id', () => {
    const props = {
      machineId: 'machine-local',
      session: createMockFileSession(),
    }

    const _withoutTerminal: FileManagerProps = props

    expect(Object.keys(props)).not.toContain('terminalId')
  })

  it('shows absolute breadcrumbs without labeling the root slash as root', async () => {
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => ({
        path,
        parent: '/',
        total: 0,
        entries: [],
      }),
    })

    render(
      <FileManager
        machineId="machine-local"
        session={session}
        initialPath="/tmp"
      />,
    )

    await waitFor(() => expect(screen.getByText('tmp')).toBeTruthy())
    expect(screen.getByRole('button', { name: '/' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'root' })).toBeNull()
  })

  it('renders markdown previews from selected files', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [{ name: 'README.md', type: 'file', size: 42 }],
      },
      '/files/preview': {
        path: '/README.md',
        name: 'README.md',
        size: 42,
        mime_type: 'text/markdown',
        category: 'text',
        is_text: true,
        content: '# Title\n\nSome `code` and **bold** text.',
      },
    }, {}, { terminalId: 'terminal-1' })

    render(fileManager(session))

    await waitFor(() => expect(screen.getByText('README.md')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /preview readme.md/i }))

    await waitFor(() => expect(screen.getByTestId('termx-file-preview')).toBeTruthy())
    expect(screen.getByRole('heading', { name: 'Title' })).toBeTruthy()
    expect(screen.getByText('code')).toBeTruthy()
    expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/preview',
      params: { path: '/README.md', max_size: 8388608 },
    })
  })

  it('previews wrapped plain text without horizontal-only code layout', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [{ name: 'app.log', type: 'file', size: 140 }],
      },
      '/files/preview': {
        path: '/app.log',
        name: 'app.log',
        size: 140,
        mime_type: 'text/plain',
        category: 'text',
        is_text: true,
        content: 'one very long line that should wrap on small mobile screens',
      },
    }, {}, { terminalId: 'terminal-1' })

    render(fileManager(session))

    await waitFor(() => expect(screen.getByText('app.log')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /preview app.log/i }))

    const text = await screen.findByText(/one very long line/)
    expect(text.className).toMatch(/whitespace-pre-wrap/)
    expect(text.className).toMatch(/break-words/)
  })

  it('opens previews as fullscreen dialogs outside the file manager container', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [{ name: 'app.log', type: 'file', size: 20 }],
      },
      '/files/preview': {
        path: '/app.log',
        name: 'app.log',
        size: 20,
        mime_type: 'text/plain',
        category: 'text',
        is_text: true,
        content: 'hello',
      },
    }, {}, { terminalId: 'terminal-1' })

    render(fileManager(session))

    await waitFor(() => expect(screen.getByText('app.log')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /preview app.log/i }))

    const preview = await screen.findByTestId('termx-file-preview')
    expect(preview.className).toMatch(/\bfixed\b/)
    expect(preview.className).toMatch(/\binset-0\b/)
    expect(screen.getByTestId('termx-file-manager').contains(preview)).toBe(false)
  })

  it('renders code previews with highlight.js markup, line numbers, and wrap toggle', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [{ name: 'app.ts', type: 'file', size: 42 }],
      },
      '/files/preview': {
        path: '/app.ts',
        name: 'app.ts',
        size: 42,
        mime_type: 'text/typescript',
        category: 'text',
        is_text: true,
        content: 'const answer = 42\nexport default answer',
      },
    }, {}, { terminalId: 'terminal-1' })

    render(fileManager(session))

    await waitFor(() => expect(screen.getByText('app.ts')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /preview app.ts/i }))

    expect(await screen.findByText('TypeScript')).toBeTruthy()
    expect(screen.getByText('1')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
    const firstLine = screen.getByTestId('termx-file-preview-line-1')
    expect(firstLine.className).toMatch(/whitespace-pre\b/)
    expect(firstLine.querySelector('.hljs-keyword')?.textContent).toBe('const')

    await userEvent.click(screen.getByRole('button', { name: /enable line wrap/i }))
    expect(screen.getByTestId('termx-file-preview-line-1').className).toMatch(/whitespace-pre-wrap/)
  })

  it('renders image previews from base64 preview content', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [{ name: 'shot.png', type: 'file', size: 68 }],
      },
      '/files/preview': {
        path: '/shot.png',
        name: 'shot.png',
        size: 68,
        mime_type: 'image/png',
        category: 'image',
        is_text: false,
        content_base64: 'iVBORw0KGgo=',
      },
    }, {}, { terminalId: 'terminal-1' })

    render(fileManager(session))

    await waitFor(() => expect(screen.getByText('shot.png')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /preview shot.png/i }))

    const image = await screen.findByRole('img', { name: 'shot.png' })
    expect(image.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo=')
  })
})

function fileManager(session: FileManagerProps['session']) {
  return (
    <FileManager
      machineId="machine-local"
      terminalId="terminal-1"
      session={session}
      initialPath="/"
    />
  )
}
