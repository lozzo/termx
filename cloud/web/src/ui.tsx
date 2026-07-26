import { X } from 'lucide-react'
import { cloneElement, isValidElement, useId, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactElement, type ReactNode } from 'react'

export function Button({ tone = 'default', className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { tone?: 'default' | 'primary' | 'danger' | 'quiet' }) {
  return <button className={`button button-${tone} ${className}`} {...props} />
}

export function IconButton({ label, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return <button className="icon-button" title={label} aria-label={label} {...props} />
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

export function TableFrame({ children }: { children: ReactNode }) { return <div className="table-frame">{children}</div> }
export function Empty({ children = '暂无数据' }: { children?: ReactNode }) { return <div className="empty-state">{children}</div> }
export function Skeleton({ rows = 6 }: { rows?: number }) { return <div className="skeleton-list" aria-label="正在加载">{Array.from({ length: rows }, (_, index) => <i key={index} />)}</div> }

export function Dialog({ title, open, onClose, children, footer }: { title: string; open: boolean; onClose: () => void; children: ReactNode; footer?: ReactNode }) {
  if (!open) return null
  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="dialog" role="dialog" aria-modal="true" aria-label={title}>
      <header><h2>{title}</h2><IconButton label="关闭" onClick={onClose}><X size={18} /></IconButton></header>
      <div className="dialog-body">{children}</div>
      {footer && <footer>{footer}</footer>}
    </section>
  </div>
}

export function ErrorState({ error }: { error: unknown }) { return <Notice tone="error">{error instanceof Error ? error.message : '加载失败'}</Notice> }
