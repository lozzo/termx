import { useEffect, useMemo, useState, type ReactNode } from 'react'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import cpp from 'highlight.js/lib/languages/cpp'
import css from 'highlight.js/lib/languages/css'
import go from 'highlight.js/lib/languages/go'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import rust from 'highlight.js/lib/languages/rust'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import { Code2, FileText, WrapText } from 'lucide-react'
import { hapticSelection } from '../../platform/haptics'
import { extension } from '../fileUtils'

hljs.registerLanguage('bash', bash)
hljs.registerLanguage('cpp', cpp)
hljs.registerLanguage('css', css)
hljs.registerLanguage('go', go)
hljs.registerLanguage('java', java)
hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('json', json)
hljs.registerLanguage('python', python)
hljs.registerLanguage('rust', rust)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('yaml', yaml)

interface CodeLanguage {
  id: string
  label: string
}

const codeLanguageByExtension: Record<string, CodeLanguage> = {
  bash: { id: 'bash', label: 'Shell' },
  c: { id: 'cpp', label: 'C' },
  cc: { id: 'cpp', label: 'C++' },
  cpp: { id: 'cpp', label: 'C++' },
  cs: { id: 'java', label: 'C#' },
  css: { id: 'css', label: 'CSS' },
  go: { id: 'go', label: 'Go' },
  h: { id: 'cpp', label: 'C/C++' },
  hpp: { id: 'cpp', label: 'C++' },
  html: { id: 'xml', label: 'HTML' },
  java: { id: 'java', label: 'Java' },
  js: { id: 'javascript', label: 'JavaScript' },
  json: { id: 'json', label: 'JSON' },
  jsx: { id: 'javascript', label: 'JSX' },
  kt: { id: 'java', label: 'Kotlin' },
  mjs: { id: 'javascript', label: 'JavaScript' },
  py: { id: 'python', label: 'Python' },
  rb: { id: 'python', label: 'Ruby' },
  rs: { id: 'rust', label: 'Rust' },
  scss: { id: 'css', label: 'SCSS' },
  sh: { id: 'bash', label: 'Shell' },
  sql: { id: 'sql', label: 'SQL' },
  ts: { id: 'typescript', label: 'TypeScript' },
  tsx: { id: 'typescript', label: 'TSX' },
  xml: { id: 'xml', label: 'XML' },
  yaml: { id: 'yaml', label: 'YAML' },
  yml: { id: 'yaml', label: 'YAML' },
  zsh: { id: 'bash', label: 'Shell' },
}

export function TextPreview({ text, name, mimeType }: { text: string; name: string; mimeType: string }) {
  const language = detectCodeLanguage(name, mimeType)
  const languageId = language?.id
  const isCode = !!languageId
  const [softWrap, setSoftWrap] = useState(() => !isCode)
  const lines = useMemo(() => normalizePreviewText(text).split('\n'), [text])
  const highlightedLines = useMemo(
    () => languageId ? lines.map((line) => highlightCodeLine(line, languageId)) : [],
    [languageId, lines],
  )

  useEffect(() => {
    setSoftWrap(!isCode)
  }, [isCode, name, mimeType])

  const wrapLabel = softWrap ? 'Disable line wrap' : 'Enable line wrap'

  return (
    <div className="flex min-h-full flex-col bg-white">
      <div className="sticky top-0 z-10 flex min-h-11 shrink-0 items-center justify-between gap-2 border-b border-zinc-200/70 bg-white px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          {isCode ? <Code2 className="h-4 w-4 shrink-0 text-zinc-500" /> : <FileText className="h-4 w-4 shrink-0 text-zinc-500" />}
          <span className="truncate text-[12px] font-semibold uppercase tracking-wide text-zinc-500">
            {language?.label ?? 'Plain text'}
          </span>
        </div>
        <button
          type="button"
          aria-label={wrapLabel}
          title={wrapLabel}
          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition-colors active:scale-95 ${softWrap ? 'bg-blue-50 text-blue-600' : 'text-zinc-600 hover:bg-zinc-50 active:bg-zinc-100'}`}
          onClick={() => { hapticSelection(); setSoftWrap((current) => !current) }}
        >
          <WrapText className="h-4 w-4" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-white">
        <div className={`py-2 font-mono text-[12px] leading-5 text-zinc-900 ${softWrap ? 'min-w-0' : 'w-max min-w-full'}`}>
          {lines.map((line, index) => (
            <div
              key={`line-${index}`}
              className={`grid min-h-5 ${softWrap ? 'grid-cols-[3.25rem_minmax(0,1fr)]' : 'grid-cols-[3.25rem_max-content]'}`}
            >
              <span className="select-none border-r border-zinc-100 pr-2 text-right text-[11px] leading-5 text-zinc-400">
                {index + 1}
              </span>
              <code
                data-testid={`termx-file-preview-line-${index + 1}`}
                className={`hljs block bg-transparent px-3 text-[12px] leading-5 ${softWrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre'}`}
                {...(isCode
                  ? { dangerouslySetInnerHTML: { __html: highlightedLines[index] ?? '' } }
                  : { children: line })}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export function MarkdownPreview({ text }: { text: string }) {
  return (
    <article className="min-h-full bg-white px-4 py-5 text-[15px] leading-7 text-zinc-800">
      {renderMarkdownBlocks(text)}
    </article>
  )
}

function normalizePreviewText(text: string): string {
  return text.replace(/\r\n?/g, '\n')
}

function detectCodeLanguage(name: string, mimeType: string): CodeLanguage | null {
  const ext = extension(name)
  const language = codeLanguageByExtension[ext]
  if (language) return language
  if (/typescript/i.test(mimeType)) return { id: 'typescript', label: 'TypeScript' }
  if (/javascript|ecmascript/i.test(mimeType)) return { id: 'javascript', label: 'JavaScript' }
  if (/json/i.test(mimeType)) return { id: 'json', label: 'JSON' }
  if (/html|xml/i.test(mimeType)) return { id: 'xml', label: mimeType.includes('xml') ? 'XML' : 'HTML' }
  if (/css/i.test(mimeType)) return { id: 'css', label: 'CSS' }
  if (/x-python/i.test(mimeType)) return { id: 'python', label: 'Python' }
  if (/x-sh|shellscript/i.test(mimeType)) return { id: 'bash', label: 'Shell' }
  return null
}

function highlightCodeLine(line: string, language: string): string {
  return hljs.highlight(line || ' ', {
    language,
    ignoreIllegals: true,
  }).value
}

function renderMarkdownBlocks(text: string) {
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  const blocks: ReactNode[] = []
  let index = 0
  let key = 0

  while (index < lines.length) {
    const line = lines[index] ?? ''
    if (!line.trim()) {
      index += 1
      continue
    }

    const fence = line.match(/^```(\S*)\s*$/)
    if (fence) {
      const code: string[] = []
      index += 1
      while (index < lines.length && !/^```\s*$/.test(lines[index] ?? '')) {
        code.push(lines[index] ?? '')
        index += 1
      }
      if (index < lines.length) index += 1
      blocks.push(
        <pre key={`code-${key++}`} className="my-4 overflow-x-auto rounded-lg bg-zinc-950 p-3 font-mono text-[12px] leading-5 text-zinc-100">
          <code>{code.join('\n')}</code>
        </pre>,
      )
      continue
    }

    const heading = line.match(/^(#{1,3})\s+(.+)$/)
    if (heading) {
      const level = heading[1]?.length ?? 1
      const content = renderInlineMarkdown(heading[2] ?? '', `h-${key}`)
      if (level === 1) {
        blocks.push(<h1 key={`h-${key++}`} className="mt-1 break-words text-[22px] font-bold leading-8 text-zinc-950">{content}</h1>)
      } else if (level === 2) {
        blocks.push(<h2 key={`h-${key++}`} className="mt-5 break-words text-[18px] font-bold leading-7 text-zinc-950">{content}</h2>)
      } else {
        blocks.push(<h3 key={`h-${key++}`} className="mt-4 break-words text-[16px] font-bold leading-7 text-zinc-900">{content}</h3>)
      }
      index += 1
      continue
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const items: ReactNode[] = []
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index] ?? '')) {
        const itemText = (lines[index] ?? '').replace(/^\s*[-*]\s+/, '')
        items.push(<li key={`li-${key}-${items.length}`} className="break-words">{renderInlineMarkdown(itemText, `li-${key}-${items.length}`)}</li>)
        index += 1
      }
      blocks.push(<ul key={`ul-${key++}`} className="my-3 list-disc space-y-1 pl-5">{items}</ul>)
      continue
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const items: ReactNode[] = []
      while (index < lines.length && /^\s*\d+\.\s+/.test(lines[index] ?? '')) {
        const itemText = (lines[index] ?? '').replace(/^\s*\d+\.\s+/, '')
        items.push(<li key={`oli-${key}-${items.length}`} className="break-words">{renderInlineMarkdown(itemText, `oli-${key}-${items.length}`)}</li>)
        index += 1
      }
      blocks.push(<ol key={`ol-${key++}`} className="my-3 list-decimal space-y-1 pl-5">{items}</ol>)
      continue
    }

    if (/^>\s?/.test(line)) {
      const quote: string[] = []
      while (index < lines.length && /^>\s?/.test(lines[index] ?? '')) {
        quote.push((lines[index] ?? '').replace(/^>\s?/, ''))
        index += 1
      }
      blocks.push(
        <blockquote key={`quote-${key++}`} className="my-3 border-l-4 border-zinc-300 pl-3 text-zinc-600">
          {renderInlineMarkdown(quote.join(' '), `quote-${key}`)}
        </blockquote>,
      )
      continue
    }

    const paragraph = [line.trim()]
    index += 1
    while (index < lines.length && (lines[index] ?? '').trim() && !isMarkdownBlockStart(lines[index] ?? '')) {
      paragraph.push((lines[index] ?? '').trim())
      index += 1
    }
    blocks.push(
      <p key={`p-${key++}`} className="my-3 break-words">
        {renderInlineMarkdown(paragraph.join(' '), `p-${key}`)}
      </p>,
    )
  }

  return blocks.length > 0 ? blocks : [<p key="empty" className="text-zinc-500">Empty file</p>]
}

function renderInlineMarkdown(text: string, keyPrefix: string) {
  const parts: ReactNode[] = []
  const tokenPattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\)|\*[^*]+\*)/g
  let lastIndex = 0
  let partIndex = 0
  for (const match of text.matchAll(tokenPattern)) {
    const token = match[0]
    const index = match.index ?? 0
    if (index > lastIndex) parts.push(text.slice(lastIndex, index))
    if (token.startsWith('`') && token.endsWith('`')) {
      parts.push(<code key={`${keyPrefix}-code-${partIndex++}`} className="rounded bg-zinc-100 px-1 py-0.5 font-mono text-[0.9em] text-zinc-900">{token.slice(1, -1)}</code>)
    } else if (token.startsWith('**') && token.endsWith('**')) {
      parts.push(<strong key={`${keyPrefix}-strong-${partIndex++}`} className="font-bold text-zinc-950">{token.slice(2, -2)}</strong>)
    } else if (token.startsWith('*') && token.endsWith('*')) {
      parts.push(<em key={`${keyPrefix}-em-${partIndex++}`}>{token.slice(1, -1)}</em>)
    } else {
      const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
      const href = link?.[2] ?? ''
      if (isSafeLink(href)) {
        parts.push(
          <a key={`${keyPrefix}-link-${partIndex++}`} className="break-all font-semibold text-blue-600 underline" href={href} target="_blank" rel="noreferrer">
            {link?.[1] ?? href}
          </a>,
        )
      } else {
        parts.push(link?.[1] ?? token)
      }
    }
    lastIndex = index + token.length
  }
  if (lastIndex < text.length) parts.push(text.slice(lastIndex))
  return parts
}

function isMarkdownBlockStart(line: string): boolean {
  return /^```/.test(line) ||
    /^#{1,3}\s+/.test(line) ||
    /^\s*[-*]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^>\s?/.test(line)
}

function isSafeLink(href: string): boolean {
  return /^(https?:|mailto:|#|\/)/i.test(href)
}
