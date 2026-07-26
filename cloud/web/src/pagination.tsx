import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { Button } from './ui'

export const pageSize = 25

export function pageURL(path: string, cursor: string, query = ''): string {
  const values = new URLSearchParams({ page_size: pageSize.toString() })
  if (cursor) values.set('cursor', cursor)
  if (query) values.set('query', query)
  return `${path}?${values}`
}

export function useCursorPagination() {
  const [history, setHistory] = useState([''])
  return {
    cursor: history[history.length - 1] ?? '',
    page: history.length,
    next: (cursor: string) => { if (cursor) setHistory((values) => [...values, cursor]) },
    previous: () => setHistory((values) => values.length > 1 ? values.slice(0, -1) : values),
    reset: () => setHistory(['']),
  }
}

export function CursorPagination({ page, nextCursor, onPrevious, onNext }: { page: number; nextCursor: string; onPrevious: () => void; onNext: (cursor: string) => void }) {
  if (page === 1 && !nextCursor) return null
  return <nav className="pagination" aria-label="列表分页">
    <Button onClick={onPrevious} disabled={page === 1}><ChevronLeft size={16} />上一页</Button>
    <span>第 {page} 页</span>
    <Button onClick={() => onNext(nextCursor)} disabled={!nextCursor}>下一页<ChevronRight size={16} /></Button>
  </nav>
}
