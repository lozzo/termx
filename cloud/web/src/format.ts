import type { Timestamp } from '@bufbuild/protobuf/wkt'
import { AccountRole, AccountState } from './generated/cloud/v1/account_pb'
import { ClientProduct } from './generated/cloud/v1/runtime_pb'
import { EntitlementState, OrderStatus, PaymentAttemptStatus, PlanState, SubscriptionState } from './generated/cloud/v1/commerce_pb'

export function dateTime(value?: Timestamp): string {
  if (!value) return '-'
  return new Date(Number(value.seconds) * 1000 + value.nanos / 1_000_000).toLocaleString('zh-CN', { hour12: false })
}

export function bytes(value: bigint | number): string {
  let amount = Number(value)
  if (!Number.isFinite(amount) || amount <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let index = 0
  while (amount >= 1024 && index < units.length - 1) { amount /= 1024; index++ }
  return `${amount >= 100 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`
}

export function money(currency: string, minorUnits: bigint): string {
  return new Intl.NumberFormat('zh-CN', { style: 'currency', currency: currency || 'CNY' }).format(Number(minorUnits) / 100)
}

export function accountState(value: AccountState): string { return value === AccountState.PENDING ? '待设置' : value === AccountState.ACTIVE ? '正常' : value === AccountState.DISABLED ? '已禁用' : '未知' }
export function role(value: AccountRole): string { return value === AccountRole.ADMIN ? '管理员' : value === AccountRole.OPERATOR ? '运营' : value === AccountRole.USER ? '用户' : '未知' }
export function planState(value: PlanState): string { return value === PlanState.PUBLISHED ? '已发布' : value === PlanState.DRAFT ? '草稿' : value === PlanState.RETIRED ? '已退休' : '未知' }
export function orderState(value: OrderStatus): string { return ({ [OrderStatus.PENDING]: '待支付', [OrderStatus.PAID]: '已支付', [OrderStatus.PAYMENT_FAILED]: '支付失败', [OrderStatus.REFUNDED]: '已退款', [OrderStatus.REVOKED]: '已撤销' } as Record<number, string>)[value] ?? '未知' }
export function attemptState(value: PaymentAttemptStatus): string { return value === PaymentAttemptStatus.SUCCEEDED ? '成功' : value === PaymentAttemptStatus.FAILED ? '失败' : value === PaymentAttemptStatus.PENDING ? '处理中' : '未知' }
export function subscriptionState(value: SubscriptionState): string { return ({ [SubscriptionState.ACTIVE]: '生效中', [SubscriptionState.CANCEL_AT_PERIOD_END]: '到期取消', [SubscriptionState.CANCELED]: '已取消', [SubscriptionState.SUSPENDED]: '已暂停', [SubscriptionState.EXPIRED]: '已到期', [SubscriptionState.PAST_DUE]: '逾期' } as Record<number, string>)[value] ?? '未知' }
export function entitlementState(value: EntitlementState): string { return value === EntitlementState.ACTIVE ? '可用' : value === EntitlementState.SUSPENDED ? '已暂停' : value === EntitlementState.EXPIRED ? '已到期' : '未知' }
export function product(value: ClientProduct): string { return ({ [ClientProduct.TUI]: 'TUI', [ClientProduct.CLI]: 'CLI', [ClientProduct.ANDROID]: 'Android', [ClientProduct.IOS]: 'iOS', [ClientProduct.DESKTOP_GUI]: '桌面 GUI' } as Record<number, string>)[value] ?? '未知客户端' }

export function compactID(value: string): string {
  if (value.length <= 18) return value || '-'
  return `${value.slice(0, 8)}…${value.slice(-6)}`
}
