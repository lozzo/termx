import type { ITheme } from '@xterm/xterm'
import type { TerminalRenderer } from './Terminal'
import type { RemoteRuntimeStorage } from './transport'

export type TerminalKeyboardMode = 'auto' | 'resize' | 'shift'
export type TerminalThemeGroup = 'dark' | 'light'

export interface TerminalThemeUi {
  page: string
  surface: string
  surfaceRaised: string
  border: string
  borderSubtle: string
  text: string
  muted: string
  faint: string
  accent: string
  accentText: string
  terminalBackground: string
  terminalForeground: string
  terminalCursor: string
  overlay: string
  scrollbar: string
  scrollbarActive: string
}

export interface TerminalFontOption {
  label: string
  value: string
}

export interface TerminalSettings {
  fontSize: number
  fontFamily: string
  themeId: TerminalThemeId
  renderer: TerminalRenderer
  keyboardMode: TerminalKeyboardMode
  scrollback: number
  cursorBlink: boolean
}

export const TERMINAL_SETTINGS_STORAGE_KEY = 'termx.terminal.settings.v1'

export const TERMX_DARK_TERMINAL_THEME: ITheme = {
  background: '#0c0c0c',
  foreground: '#f4f4f5',
  cursor: '#d4d4d8',
  selectionBackground: '#3f3f46',
  black: '#18181b',
  red: '#ef4444',
  green: '#22c55e',
  yellow: '#eab308',
  blue: '#3b82f6',
  magenta: '#d946ef',
  cyan: '#06b6d4',
  white: '#f4f4f5',
  brightBlack: '#71717a',
  brightRed: '#f87171',
  brightGreen: '#4ade80',
  brightYellow: '#fde047',
  brightBlue: '#60a5fa',
  brightMagenta: '#e879f9',
  brightCyan: '#22d3ee',
  brightWhite: '#fafafa',
}

interface TerminalThemeOptionDefinition {
  id: string
  label: string
  group: TerminalThemeGroup
  theme: ITheme
}

export const TERMINAL_THEME_OPTIONS = [
  {
    id: 'termx-dark',
    label: 'TermX Dark',
    group: 'dark',
    theme: TERMX_DARK_TERMINAL_THEME,
  },
  {
    id: 'tokyo-night',
    label: 'Tokyo Night',
    group: 'dark',
    theme: {
      background: '#1a1b26',
      foreground: '#a9b1d6',
      cursor: '#c0caf5',
      selectionBackground: '#33467c',
      black: '#15161e',
      red: '#f7768e',
      green: '#9ece6a',
      yellow: '#e0af68',
      blue: '#7aa2f7',
      magenta: '#bb9af7',
      cyan: '#7dcfff',
      white: '#a9b1d6',
      brightBlack: '#414868',
      brightRed: '#f7768e',
      brightGreen: '#9ece6a',
      brightYellow: '#e0af68',
      brightBlue: '#7aa2f7',
      brightMagenta: '#bb9af7',
      brightCyan: '#7dcfff',
      brightWhite: '#c0caf5',
    },
  },
  {
    id: 'dracula',
    label: 'Dracula',
    group: 'dark',
    theme: {
      background: '#282a36',
      foreground: '#f8f8f2',
      cursor: '#f8f8f2',
      selectionBackground: '#44475a',
      black: '#21222c',
      red: '#ff5555',
      green: '#50fa7b',
      yellow: '#f1fa8c',
      blue: '#bd93f9',
      magenta: '#ff79c6',
      cyan: '#8be9fd',
      white: '#f8f8f2',
      brightBlack: '#6272a4',
      brightRed: '#ff6e6e',
      brightGreen: '#69ff94',
      brightYellow: '#ffffa5',
      brightBlue: '#d6acff',
      brightMagenta: '#ff92df',
      brightCyan: '#a4ffff',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'one-dark',
    label: 'One Dark',
    group: 'dark',
    theme: {
      background: '#282c34',
      foreground: '#abb2bf',
      cursor: '#528bff',
      selectionBackground: '#3e4451',
      black: '#1e2127',
      red: '#e06c75',
      green: '#98c379',
      yellow: '#d19a66',
      blue: '#61afef',
      magenta: '#c678dd',
      cyan: '#56b6c2',
      white: '#abb2bf',
      brightBlack: '#5c6370',
      brightRed: '#e06c75',
      brightGreen: '#98c379',
      brightYellow: '#d19a66',
      brightBlue: '#61afef',
      brightMagenta: '#c678dd',
      brightCyan: '#56b6c2',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'catppuccin-mocha',
    label: 'Catppuccin Mocha',
    group: 'dark',
    theme: {
      background: '#1e1e2e',
      foreground: '#cdd6f4',
      cursor: '#f5e0dc',
      selectionBackground: '#45475a',
      black: '#45475a',
      red: '#f38ba8',
      green: '#a6e3a1',
      yellow: '#f9e2af',
      blue: '#89b4fa',
      magenta: '#f5c2e7',
      cyan: '#94e2d5',
      white: '#bac2de',
      brightBlack: '#585b70',
      brightRed: '#f38ba8',
      brightGreen: '#a6e3a1',
      brightYellow: '#f9e2af',
      brightBlue: '#89b4fa',
      brightMagenta: '#f5c2e7',
      brightCyan: '#94e2d5',
      brightWhite: '#a6adc8',
    },
  },
  {
    id: 'solarized-dark',
    label: 'Solarized Dark',
    group: 'dark',
    theme: {
      background: '#002b36',
      foreground: '#839496',
      cursor: '#839496',
      selectionBackground: '#073642',
      black: '#073642',
      red: '#dc322f',
      green: '#859900',
      yellow: '#b58900',
      blue: '#268bd2',
      magenta: '#d33682',
      cyan: '#2aa198',
      white: '#eee8d5',
      brightBlack: '#586e75',
      brightRed: '#cb4b16',
      brightGreen: '#586e75',
      brightYellow: '#657b83',
      brightBlue: '#839496',
      brightMagenta: '#6c71c4',
      brightCyan: '#93a1a1',
      brightWhite: '#fdf6e3',
    },
  },
  {
    id: 'nord',
    label: 'Nord',
    group: 'dark',
    theme: {
      background: '#2e3440',
      foreground: '#d8dee9',
      cursor: '#d8dee9',
      selectionBackground: '#434c5e',
      black: '#3b4252',
      red: '#bf616a',
      green: '#a3be8c',
      yellow: '#ebcb8b',
      blue: '#81a1c1',
      magenta: '#b48ead',
      cyan: '#88c0d0',
      white: '#e5e9f0',
      brightBlack: '#4c566a',
      brightRed: '#bf616a',
      brightGreen: '#a3be8c',
      brightYellow: '#ebcb8b',
      brightBlue: '#81a1c1',
      brightMagenta: '#b48ead',
      brightCyan: '#8fbcbb',
      brightWhite: '#eceff4',
    },
  },
  {
    id: 'gruvbox-dark',
    label: 'Gruvbox Dark',
    group: 'dark',
    theme: {
      background: '#282828',
      foreground: '#ebdbb2',
      cursor: '#ebdbb2',
      selectionBackground: '#504945',
      black: '#282828',
      red: '#cc241d',
      green: '#98971a',
      yellow: '#d79921',
      blue: '#458588',
      magenta: '#b16286',
      cyan: '#689d6a',
      white: '#a89984',
      brightBlack: '#928374',
      brightRed: '#fb4934',
      brightGreen: '#b8bb26',
      brightYellow: '#fabd2f',
      brightBlue: '#83a598',
      brightMagenta: '#d3869b',
      brightCyan: '#8ec07c',
      brightWhite: '#ebdbb2',
    },
  },
  {
    id: 'github-dark',
    label: 'GitHub Dark',
    group: 'dark',
    theme: {
      background: '#0d1117',
      foreground: '#c9d1d9',
      cursor: '#58a6ff',
      selectionBackground: '#264f78',
      black: '#484f58',
      red: '#ff7b72',
      green: '#3fb950',
      yellow: '#d29922',
      blue: '#58a6ff',
      magenta: '#bc8cff',
      cyan: '#39c5cf',
      white: '#b1bac4',
      brightBlack: '#6e7681',
      brightRed: '#ffa198',
      brightGreen: '#56d364',
      brightYellow: '#e3b341',
      brightBlue: '#79c0ff',
      brightMagenta: '#d2a8ff',
      brightCyan: '#56d4dd',
      brightWhite: '#f0f6fc',
    },
  },
  {
    id: 'github-light',
    label: 'GitHub Light',
    group: 'light',
    theme: {
      background: '#ffffff',
      foreground: '#24292f',
      cursor: '#044289',
      selectionBackground: '#b6d4fe',
      black: '#24292f',
      red: '#cf222e',
      green: '#116329',
      yellow: '#4d2d00',
      blue: '#0969da',
      magenta: '#8250df',
      cyan: '#1b7c83',
      white: '#6e7781',
      brightBlack: '#57606a',
      brightRed: '#a40e26',
      brightGreen: '#1a7f37',
      brightYellow: '#633c01',
      brightBlue: '#218bff',
      brightMagenta: '#a475f9',
      brightCyan: '#3192aa',
      brightWhite: '#8c959f',
    },
  },
  {
    id: 'solarized-light',
    label: 'Solarized Light',
    group: 'light',
    theme: {
      background: '#fdf6e3',
      foreground: '#657b83',
      cursor: '#586e75',
      selectionBackground: '#eee8d5',
      black: '#073642',
      red: '#dc322f',
      green: '#859900',
      yellow: '#b58900',
      blue: '#268bd2',
      magenta: '#d33682',
      cyan: '#2aa198',
      white: '#eee8d5',
      brightBlack: '#002b36',
      brightRed: '#cb4b16',
      brightGreen: '#586e75',
      brightYellow: '#657b83',
      brightBlue: '#839496',
      brightMagenta: '#6c71c4',
      brightCyan: '#93a1a1',
      brightWhite: '#fdf6e3',
    },
  },
  {
    id: 'catppuccin-latte',
    label: 'Catppuccin Latte',
    group: 'light',
    theme: {
      background: '#eff1f5',
      foreground: '#4c4f69',
      cursor: '#dc8a78',
      selectionBackground: '#ccd0da',
      black: '#5c5f77',
      red: '#d20f39',
      green: '#40a02b',
      yellow: '#df8e1d',
      blue: '#1e66f5',
      magenta: '#ea76cb',
      cyan: '#179299',
      white: '#acb0be',
      brightBlack: '#6c6f85',
      brightRed: '#d20f39',
      brightGreen: '#40a02b',
      brightYellow: '#df8e1d',
      brightBlue: '#1e66f5',
      brightMagenta: '#ea76cb',
      brightCyan: '#179299',
      brightWhite: '#bcc0cc',
    },
  },
  {
    id: 'one-light',
    label: 'One Light',
    group: 'light',
    theme: {
      background: '#fafafa',
      foreground: '#383a42',
      cursor: '#526fff',
      selectionBackground: '#bfceff',
      black: '#383a42',
      red: '#e45649',
      green: '#50a14f',
      yellow: '#c18401',
      blue: '#4078f2',
      magenta: '#a626a4',
      cyan: '#0184bc',
      white: '#a0a1a7',
      brightBlack: '#696c77',
      brightRed: '#e45649',
      brightGreen: '#50a14f',
      brightYellow: '#c18401',
      brightBlue: '#4078f2',
      brightMagenta: '#a626a4',
      brightCyan: '#0184bc',
      brightWhite: '#fafafa',
    },
  },
  {
    id: 'nord-light',
    label: 'Nord Light',
    group: 'light',
    theme: {
      background: '#eceff4',
      foreground: '#2e3440',
      cursor: '#2e3440',
      selectionBackground: '#d8dee9',
      black: '#2e3440',
      red: '#bf616a',
      green: '#a3be8c',
      yellow: '#ebcb8b',
      blue: '#81a1c1',
      magenta: '#b48ead',
      cyan: '#88c0d0',
      white: '#d8dee9',
      brightBlack: '#4c566a',
      brightRed: '#bf616a',
      brightGreen: '#a3be8c',
      brightYellow: '#ebcb8b',
      brightBlue: '#81a1c1',
      brightMagenta: '#b48ead',
      brightCyan: '#8fbcbb',
      brightWhite: '#eceff4',
    },
  },
  {
    id: 'gruvbox-light',
    label: 'Gruvbox Light',
    group: 'light',
    theme: {
      background: '#fbf1c7',
      foreground: '#3c3836',
      cursor: '#3c3836',
      selectionBackground: '#d5c4a1',
      black: '#3c3836',
      red: '#cc241d',
      green: '#98971a',
      yellow: '#d79921',
      blue: '#458588',
      magenta: '#b16286',
      cyan: '#689d6a',
      white: '#7c6f64',
      brightBlack: '#928374',
      brightRed: '#9d0006',
      brightGreen: '#79740e',
      brightYellow: '#b57614',
      brightBlue: '#076678',
      brightMagenta: '#8f3f71',
      brightCyan: '#427b58',
      brightWhite: '#3c3836',
    },
  },
  {
    id: 'rose-pine-dawn',
    label: 'Rose Pine Dawn',
    group: 'light',
    theme: {
      background: '#faf4ed',
      foreground: '#575279',
      cursor: '#575279',
      selectionBackground: '#dfdad9',
      black: '#575279',
      red: '#b4637a',
      green: '#286983',
      yellow: '#ea9d34',
      blue: '#56949f',
      magenta: '#907aa9',
      cyan: '#d7827e',
      white: '#f2e9e1',
      brightBlack: '#797593',
      brightRed: '#b4637a',
      brightGreen: '#286983',
      brightYellow: '#ea9d34',
      brightBlue: '#56949f',
      brightMagenta: '#907aa9',
      brightCyan: '#d7827e',
      brightWhite: '#faf4ed',
    },
  },
  {
    id: 'everforest-light',
    label: 'Everforest Light',
    group: 'light',
    theme: {
      background: '#f3ead3',
      foreground: '#5c6a72',
      cursor: '#5c6a72',
      selectionBackground: '#e0dcc7',
      black: '#5c6a72',
      red: '#f85552',
      green: '#8da101',
      yellow: '#dfa000',
      blue: '#3a94c5',
      magenta: '#df69ba',
      cyan: '#35a77c',
      white: '#dfddc8',
      brightBlack: '#829181',
      brightRed: '#f85552',
      brightGreen: '#8da101',
      brightYellow: '#dfa000',
      brightBlue: '#3a94c5',
      brightMagenta: '#df69ba',
      brightCyan: '#35a77c',
      brightWhite: '#f3ead3',
    },
  },
] as const satisfies readonly TerminalThemeOptionDefinition[]

export type TerminalThemeOption = (typeof TERMINAL_THEME_OPTIONS)[number]
export type TerminalThemeId = TerminalThemeOption['id']

export const TERMINAL_FONT_OPTIONS: TerminalFontOption[] = [
  { label: 'JetBrains Mono NF', value: '"JetBrainsMono NF", monospace' },
  { label: 'FiraCode NF', value: '"FiraCode NF", monospace' },
  { label: 'Cascadia Code NF', value: '"CascadiaCode NF", monospace' },
  { label: 'Hack NF', value: '"Hack NF", monospace' },
  { label: 'Iosevka NF', value: '"Iosevka NF", monospace' },
  { label: 'Menlo', value: 'Menlo, Monaco, "Courier New", monospace' },
  { label: 'Courier New', value: '"Courier New", Courier, monospace' },
]

export const DEFAULT_TERMINAL_SETTINGS: TerminalSettings = {
  fontSize: 14,
  fontFamily: TERMINAL_FONT_OPTIONS[0]!.value,
  themeId: 'termx-dark',
  renderer: 'auto',
  keyboardMode: 'auto',
  scrollback: 10000,
  cursorBlink: true,
}

export function readTerminalSettings(storage: Pick<Storage, 'getItem'> | RemoteRuntimeStorage | undefined = browserStorage()): TerminalSettings {
  const migrated = migrateLegacyTerminalSettings(storage)
  const raw = readStorageValue(storage, TERMINAL_SETTINGS_STORAGE_KEY)
  if (!raw) return migrated
  try {
    return normalizeTerminalSettings({
      ...migrated,
      ...JSON.parse(raw),
    })
  } catch {
    return migrated
  }
}

export function writeTerminalSettings(
  settings: TerminalSettings,
  storage: Pick<Storage, 'setItem'> | RemoteRuntimeStorage | undefined = browserStorage(),
): TerminalSettings {
  const normalized = normalizeTerminalSettings(settings)
  try {
    storage?.setItem(TERMINAL_SETTINGS_STORAGE_KEY, JSON.stringify(normalized))
  } catch {}
  return normalized
}

export function resolveTerminalTheme(themeId: TerminalThemeId | string | undefined): ITheme {
  return TERMINAL_THEME_OPTIONS.find((option) => option.id === themeId)?.theme ?? TERMX_DARK_TERMINAL_THEME
}

export function resolveTerminalThemeOption(themeId: TerminalThemeId | string | undefined): TerminalThemeOption {
  return TERMINAL_THEME_OPTIONS.find((option) => option.id === themeId) ?? TERMINAL_THEME_OPTIONS[0]!
}

export function resolveTerminalThemeUi(themeId: TerminalThemeId | string | undefined): TerminalThemeUi {
  const option = resolveTerminalThemeOption(themeId)
  const theme = option.theme
  const background = colorValue(theme.background, TERMX_DARK_TERMINAL_THEME.background!)
  const foreground = colorValue(theme.foreground, TERMX_DARK_TERMINAL_THEME.foreground!)
  const cursor = colorValue(theme.cursor, foreground)
  const accent = colorValue(theme.blue ?? theme.cyan ?? theme.brightBlue, cursor)
  const muted = colorValue(theme.brightBlack ?? theme.black, option.group === 'dark' ? '#71717a' : '#6b7280')
  const selection = colorValue(theme.selectionBackground, option.group === 'dark' ? '#3f3f46' : '#d4d4d8')

  if (option.group === 'light') {
    return {
      page: background,
      surface: '#ffffff',
      surfaceRaised: colorValue(theme.white, selection),
      border: selection,
      borderSubtle: withAlpha(muted, 0.22),
      text: foreground,
      muted,
      faint: withAlpha(muted, 0.68),
      accent,
      accentText: '#ffffff',
      terminalBackground: background,
      terminalForeground: foreground,
      terminalCursor: cursor,
      overlay: withAlpha('#111827', 0.42),
      scrollbar: withAlpha(muted, 0.42),
      scrollbarActive: withAlpha(muted, 0.68),
    }
  }

  return {
    page: background,
    surface: colorValue(theme.black, background),
    surfaceRaised: selection,
    border: colorValue(theme.brightBlack ?? theme.black, selection),
    borderSubtle: withAlpha(foreground, 0.12),
    text: foreground,
    muted,
    faint: withAlpha(muted, 0.72),
    accent,
    accentText: '#ffffff',
    terminalBackground: background,
    terminalForeground: foreground,
    terminalCursor: cursor,
    overlay: withAlpha(background, 0.72),
    scrollbar: withAlpha(foreground, 0.35),
    scrollbarActive: withAlpha(foreground, 0.58),
  }
}

export function terminalThemeCssVariables(themeId: TerminalThemeId | string | undefined): Record<string, string> {
  const ui = resolveTerminalThemeUi(themeId)
  return {
    '--termx-bg': ui.page,
    '--termx-surface': ui.surface,
    '--termx-surface-raised': ui.surfaceRaised,
    '--termx-border': ui.border,
    '--termx-border-subtle': ui.borderSubtle,
    '--termx-text': ui.text,
    '--termx-muted': ui.muted,
    '--termx-faint': ui.faint,
    '--termx-accent': ui.accent,
    '--termx-accent-text': ui.accentText,
    '--termx-terminal-bg': ui.terminalBackground,
    '--termx-terminal-fg': ui.terminalForeground,
    '--termx-terminal-cursor': ui.terminalCursor,
    '--termx-overlay': ui.overlay,
    '--termx-scrollbar': ui.scrollbar,
    '--termx-scrollbar-active': ui.scrollbarActive,
  }
}

export function normalizeTerminalSettings(input: Partial<TerminalSettings> | Record<string, unknown>): TerminalSettings {
  return {
    fontSize: clampNumber(input.fontSize, DEFAULT_TERMINAL_SETTINGS.fontSize, 8, 32),
    fontFamily: typeof input.fontFamily === 'string' && input.fontFamily.trim()
      ? input.fontFamily
      : DEFAULT_TERMINAL_SETTINGS.fontFamily,
    themeId: isTerminalThemeId(input.themeId) ? input.themeId : DEFAULT_TERMINAL_SETTINGS.themeId,
    renderer: isTerminalRenderer(input.renderer) ? input.renderer : DEFAULT_TERMINAL_SETTINGS.renderer,
    keyboardMode: isTerminalKeyboardMode(input.keyboardMode) ? input.keyboardMode : DEFAULT_TERMINAL_SETTINGS.keyboardMode,
    scrollback: clampNumber(input.scrollback, DEFAULT_TERMINAL_SETTINGS.scrollback, 500, 50000),
    cursorBlink: typeof input.cursorBlink === 'boolean' ? input.cursorBlink : DEFAULT_TERMINAL_SETTINGS.cursorBlink,
  }
}

function migrateLegacyTerminalSettings(storage: Pick<Storage, 'getItem'> | RemoteRuntimeStorage | undefined): TerminalSettings {
  const base = { ...DEFAULT_TERMINAL_SETTINGS }
  const legacyRenderer = readStorageValue(storage, 'termx.renderer')
  if (isTerminalRenderer(legacyRenderer)) base.renderer = legacyRenderer
  const legacyFontSizeValue = readStorageValue(storage, 'termx.fontSize')
  const legacyFontSize = legacyFontSizeValue === null ? Number.NaN : Number(legacyFontSizeValue)
  if (Number.isFinite(legacyFontSize)) base.fontSize = legacyFontSize
  return normalizeTerminalSettings(base)
}

function readStorageValue(storage: Pick<Storage, 'getItem'> | RemoteRuntimeStorage | undefined, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null
  } catch {
    return null
  }
}

function browserStorage(): Storage | undefined {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}

function clampNumber(value: unknown, fallback: number, min: number, max: number): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback
  return Math.max(min, Math.min(max, Math.round(value)))
}

function isTerminalRenderer(value: unknown): value is TerminalRenderer {
  return value === 'auto' || value === 'webgl' || value === 'canvas' || value === 'dom'
}

function isTerminalKeyboardMode(value: unknown): value is TerminalKeyboardMode {
  return value === 'auto' || value === 'resize' || value === 'shift'
}

function isTerminalThemeId(value: unknown): value is TerminalThemeId {
  return typeof value === 'string' && TERMINAL_THEME_OPTIONS.some((option) => option.id === value)
}

function colorValue(value: unknown, fallback: string): string {
  return typeof value === 'string' && value.trim() ? value : fallback
}

function withAlpha(color: string, alpha: number): string {
  const hex = color.trim()
  const normalized = hex.startsWith('#') ? hex.slice(1) : hex
  if (/^[0-9a-fA-F]{3}$/.test(normalized)) {
    const [r, g, b] = normalized.split('').map((part) => parseInt(part + part, 16))
    return `rgba(${r}, ${g}, ${b}, ${alpha})`
  }
  if (/^[0-9a-fA-F]{6}$/.test(normalized)) {
    const r = parseInt(normalized.slice(0, 2), 16)
    const g = parseInt(normalized.slice(2, 4), 16)
    const b = parseInt(normalized.slice(4, 6), 16)
    return `rgba(${r}, ${g}, ${b}, ${alpha})`
  }
  return color
}
