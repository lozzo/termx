import type {
  CoreV2HistoryCellStyle,
  CoreV2HistoryRow,
} from './coreV2TerminalProtocol'

export function coreV2HistoryRowsANSI(rows: CoreV2HistoryRow[], cols: number): string {
  return rows.map((row) => {
    let currentStyle = ''
    let width = 0
    let output = ''
    for (const cell of row.cells) {
      const style = styleANSI(cell.style)
      if (style !== currentStyle) {
        output += `\u001b[0m${style}`
        currentStyle = style
      }
      const cellWidth = Math.max(1, cell.width)
      output += cell.text || ' '.repeat(cellWidth)
      width += cellWidth
    }
    if (cols > width) {
      output += `\u001b[0m${styleANSI(row.tailFillStyle)}${' '.repeat(cols - width)}`
    }
    return `${output}\u001b[0m`
  }).join('\r\n')
}

function styleANSI(style: CoreV2HistoryCellStyle | undefined): string {
  if (!style) return ''
  const codes: string[] = []
  if (style.bold) codes.push('1')
  if (style.italic) codes.push('3')
  if (style.underline) codes.push('4')
  if (style.blink) codes.push('5')
  if (style.reverse) codes.push('7')
  if (style.strikethrough) codes.push('9')
  const foreground = colorANSI(style.fg, true)
  const background = colorANSI(style.bg, false)
  if (foreground) codes.push(foreground)
  if (background) codes.push(background)
  return codes.length > 0 ? `\u001b[${codes.join(';')}m` : ''
}

function colorANSI(value: string | undefined, foreground: boolean): string {
  const token = value?.trim() ?? ''
  if (!token) return ''
  const indexed = /^(?:(?:ansi|idx):)?(\d{1,3})$/i.exec(token)
  if (indexed) {
    const index = Math.min(255, Number(indexed[1]))
    return `${foreground ? 38 : 48};5;${index}`
  }
  const rgb = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(token)
  if (!rgb) return ''
  return `${foreground ? 38 : 48};2;${parseInt(rgb[1]!, 16)};${parseInt(rgb[2]!, 16)};${parseInt(rgb[3]!, 16)}`
}
