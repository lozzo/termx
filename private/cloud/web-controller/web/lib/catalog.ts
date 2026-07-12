import 'server-only'

export type PriceMode = 'included' | 'configured' | 'contact'

export interface CatalogPlan {
  id: string
  name: string
  eyebrow: string
  description: string
  price: {
    mode: PriceMode
    label: string
    monthly_minor?: number
    yearly_minor?: number
  }
  cta: { label: string; href: string }
  featured: boolean
  features: string[]
}

export interface Catalog {
  version: number
  currency: string
  plans: CatalogPlan[]
}

/** loadCatalog 通过 loopback Go BFF 读取产品配置；浏览器 bundle 和 Next 进程都不复制价格真值。 */
export async function loadCatalog(): Promise<Catalog> {
  const upstream = process.env.WEB_CONTROLLER_BFF_URL?.replace(/\/$/, '')
  if (!upstream) throw new Error('WEB_CONTROLLER_BFF_URL is required')
  const response = await fetch(`${upstream}/v1/catalog`, { cache: 'no-store', signal: AbortSignal.timeout(2500) })
  if (!response.ok) throw new Error(`Web Controller BFF catalog returned ${response.status}`)
  return response.json() as Promise<Catalog>
}

/** formatPlanPrice 只格式化明确发布的金额；未发布套餐保持配置标签。 */
export function formatPlanPrice(plan: CatalogPlan, currency: string): string {
  if (plan.price.mode !== 'configured' || plan.price.monthly_minor === undefined) return plan.price.label
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency,
    maximumFractionDigits: 0,
  }).format(plan.price.monthly_minor / 100)
}

/** planPriceNote 把配置状态转换成用户承诺，不向页面泄漏内部配置术语。 */
export function planPriceNote(plan: CatalogPlan): string {
  if (plan.price.mode === 'configured') return '/ month'
  if (plan.price.mode === 'included') return 'No card required'
  return plan.id === 'pro' ? 'Preview access' : 'Custom agreement'
}
