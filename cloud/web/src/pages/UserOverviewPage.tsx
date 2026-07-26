import { Activity, ArrowRight, CreditCard, MonitorSmartphone, ReceiptText } from 'lucide-react'
import { Link } from 'react-router'
import { bytes, dateTime, subscriptionState } from '../format'
import { GetAccountCommerceResponseSchema, OrderStatus } from '../generated/cloud/v1/commerce_pb'
import { ListMyDaemonsResponseSchema } from '../generated/cloud/v1/enrollment_pb'
import { useProtoQuery } from '../query'
import { useCloudAccount } from '../shell/CloudShell'
import { ErrorState, PageHeader, Skeleton, Status } from '../ui'

export function UserOverviewPage() {
  const { current } = useCloudAccount()
  const commerce = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  const daemons = useProtoQuery(['user', 'daemons'], '/api/daemons', ListMyDaemonsResponseSchema, 8_000)
  if (commerce.isPending || daemons.isPending) return <><PageHeader title={`你好，${current.account?.displayName ?? ''}`} meta="正在读取你的 Cloud 状态" /><Skeleton rows={7} /></>
  if (commerce.error || daemons.error) return <ErrorState error={commerce.error ?? daemons.error} />
  const online = daemons.data?.daemons.filter((value) => value.runtime?.online).length ?? 0
  const total = daemons.data?.daemons.filter((value) => !value.daemon?.revoked).length ?? 0
  const pending = commerce.data?.orders.filter((value) => value.status === OrderStatus.PENDING).length ?? 0
  const subscription = commerce.data?.subscription
  const usage = commerce.data?.usage
  return <>
    <PageHeader title={`你好，${current.account?.displayName ?? ''}`} meta="这里汇总你的设备、订阅和当前周期用量" />
    <section className="user-summary-strip">
      <Link to="/app/devices"><MonitorSmartphone size={20} /><span>在线设备</span><strong>{online}/{total}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/subscription"><CreditCard size={20} /><span>当前套餐</span><strong>{subscription?.planId ?? '-'}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/usage"><Activity size={20} /><span>Relay 用量</span><strong>{bytes(usage?.relayTotalBytes ?? 0n)}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/orders"><ReceiptText size={20} /><span>待支付订单</span><strong>{pending}</strong><ArrowRight size={16} /></Link>
    </section>
    <div className="user-overview-grid">
      <section className="plain-section"><header><div><h2>设备状态</h2><p>实时位置只来自当前 Edge 上报</p></div><Link to="/app/devices">管理设备</Link></header><div className="device-rows">{!daemons.data?.daemons.length ? <p className="empty-inline">还没有设备，先生成 daemon 注册命令。</p> : daemons.data.daemons.slice(0, 4).map((value) => <div key={value.daemon?.daemonId}><span><strong>{value.daemon?.displayName}</strong><small>{value.runtime?.online ? `${value.runtime.edgeName} · ${value.runtime.edgeRegion}` : '当前离线'}</small></span><Status active={Boolean(value.runtime?.online)}>{value.runtime?.online ? '在线' : value.daemon?.revoked ? '已撤销' : '离线'}</Status></div>)}</div></section>
      <section className="plain-section"><header><div><h2>订阅周期</h2><p>{subscription ? `${dateTime(subscription.periodStart)} 至 ${dateTime(subscription.periodEnd)}` : '暂无订阅'}</p></div>{subscription && <Status active>{subscriptionState(subscription.state)}</Status>}</header><dl className="quota-list"><div><dt>Cloud daemon</dt><dd>{total} / {commerce.data?.entitlement?.capability?.cloudDaemonLimit ?? 0}</dd></div><div><dt>P2P 并发</dt><dd>{commerce.data?.entitlement?.capability?.managedP2pMaxConcurrency ?? 0}</dd></div><div><dt>Relay 剩余</dt><dd>{bytes(commerce.data?.entitlement?.relayRemainingBytes ?? 0n)}</dd></div></dl><Link className="text-action" to="/app/subscription">查看套餐<ArrowRight size={15} /></Link></section>
    </div>
  </>
}
