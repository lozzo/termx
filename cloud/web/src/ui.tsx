import { ArrowLeftRight, X } from 'lucide-react'
import { ModalSurface } from '@anytty/ui/modal'
import { cloneElement, isValidElement, useId, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactElement, type ReactNode } from 'react'

export function Button({ tone = 'default', className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { tone?: 'default' | 'primary' | 'danger' | 'quiet' }) {
  return <button className={`button button-${tone} ${className}`} {...props} />
}

export function IconButton({ label, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return <button className={`icon-button ${className}`.trim()} title={label} aria-label={label} {...props} />
}

export function Field({ label, hint, htmlFor, children }: { label: string; hint?: string; htmlFor?: string; children: ReactNode }) {
  const generatedID = useId()
  const controlID = htmlFor ?? generatedID
  const control = !htmlFor && isValidElement(children)
    ? cloneElement(children as ReactElement<{ id?: string }>, { id: (children.props as { id?: string }).id ?? controlID })
    : children
  return <div className="field"><label htmlFor={controlID}>{label}</label>{control}{hint && <small>{hint}</small>}</div>
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) { return <input className="input" {...props} /> }

export function PageHeader({ title, meta, actions }: { title: string; meta?: string; actions?: ReactNode }) {
  return <div className="page-header"><div><h1>{title}</h1>{meta && <p>{meta}</p>}</div>{actions && <div className="page-actions">{actions}</div>}</div>
}

export function Notice({ children, tone = 'info' }: { children: ReactNode; tone?: 'info' | 'error' | 'warning' }) {
  return <div className={`notice notice-${tone}`} role={tone === 'error' ? 'alert' : 'status'}>{children}</div>
}

export function Status({ active, children }: { active: boolean; children: ReactNode }) {
  return <span className={`status ${active ? 'status-active' : 'status-muted'}`}><i />{children}</span>
}

export function TableFrame({ children }: { children: ReactNode }) {
  const hintID = useId()
  return <div className="table-region">
    <p className="table-scroll-hint" id={hintID}><ArrowLeftRight size={15} aria-hidden="true" />横向滚动查看更多列</p>
    <div className="table-frame" role="region" aria-label="数据表格" aria-describedby={hintID} tabIndex={0}>{children}</div>
  </div>
}
export function Empty({ children = '暂无数据' }: { children?: ReactNode }) { return <div className="empty-state">{children}</div> }
export function Skeleton({ rows = 6 }: { rows?: number }) {
  return <div className="skeleton-list" role="status" aria-live="polite" aria-busy="true">
    <span className="visually-hidden">正在加载</span>
    <span className="skeleton-decoration" aria-hidden="true">{Array.from({ length: rows }, (_, index) => <i key={index} />)}</span>
  </div>
}

export function Dialog({ title, open, onClose, children, footer, closable = true }: { title: string; open: boolean; onClose: () => void; children: ReactNode; footer?: ReactNode; closable?: boolean }) {
  if (!open) return null
  const requestClose = () => { if (closable) onClose() }
  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) requestClose() }}>
    <ModalSurface className="dialog" aria-label={title} onRequestClose={requestClose}>
      <header><h2>{title}</h2><IconButton label="关闭" onClick={requestClose} disabled={!closable}><X size={18} /></IconButton></header>
      <div className="dialog-body">{children}</div>
      {footer && <footer>{footer}</footer>}
    </ModalSurface>
  </div>
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const status = typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number' ? error.status : 0
  const correlationID = typeof error === 'object' && error !== null && 'correlationID' in error && typeof error.correlationID === 'string' ? error.correlationID : ''
  const message = status === 403
    ? '没有权限读取此内容。'
    : status === 404
      ? '请求的内容不存在或已被移除。'
      : status === 429
        ? '请求过于频繁，请稍后重试。'
        : status >= 500
          ? '服务暂时不可用，请稍后重试。'
          : '加载失败，请稍后重试。'
  return <div className="error-state"><Notice tone="error"><span>{message}</span>{correlationID && <small>关联 ID：{correlationID}</small>}</Notice><Button type="button" onClick={onRetry}>重试</Button></div>
}
