import { Activity, ArrowRight, CreditCard, MonitorSmartphone, Plus, ReceiptText, ShieldCheck, Smartphone, Wifi, WifiOff } from 'lucide-react'
import { Link } from 'react-router'
import { bytes, dateTime, subscriptionState } from '../format'
import { GetAccountCommerceResponseSchema, ListPlansResponseSchema, OrderStatus } from '../generated/cloud/v1/commerce_pb'
import { ListMyDaemonsResponseSchema } from '../generated/cloud/v1/enrollment_pb'
import { useProtoQuery } from '../query'
import { useCloudAccount } from '../shell/CloudShell'
import { ErrorState, PageHeader, Skeleton, Status } from '../ui'

export function UserOverviewPage() {
  const { current } = useCloudAccount()
  const commerce = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  const daemons = useProtoQuery(['user', 'daemons'], '/api/daemons', ListMyDaemonsResponseSchema, 8_000)
  const plans = useProtoQuery(['user', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 60_000)
  if (commerce.isPending || daemons.isPending || plans.isPending) return <><PageHeader title={`你好，${current.account?.displayName ?? ''}`} meta="正在读取你的 Cloud 状态" /><Skeleton rows={7} /></>
  if (commerce.error || daemons.error || plans.error) return <ErrorState error={commerce.error ?? daemons.error ?? plans.error} />
  const online = daemons.data?.daemons.filter((value) => value.runtime?.online).length ?? 0
  const total = daemons.data?.daemons.filter((value) => !value.daemon?.revoked).length ?? 0
  const pending = commerce.data?.orders.filter((value) => value.status === OrderStatus.PENDING).length ?? 0
  const subscription = commerce.data?.subscription
  const usage = commerce.data?.usage
  const currentPlan = plans.data?.plans.find((value) => value.planId === subscription?.planId && value.version === subscription.planVersion)
  return <>
    <section className="account-hero">
      <div className="account-hero-copy"><span className="account-kicker"><ShieldCheck size={15} />Muxvia Cloud 已就绪</span><h1>你好，{current.account?.displayName ?? ''}</h1><p>{total === 0 ? '添加第一台设备后，就能在 Muxvia App 中从外网安全连接。' : `${online} 台设备在线。打开 Muxvia App，即可继续上次的终端工作。`}</p><div className="account-hero-actions"><Link className="button button-primary" to="/app/devices">{total === 0 ? <><Plus size={17} />添加第一台设备</> : <><MonitorSmartphone size={17} />管理设备</>}</Link><Link className="button" to="/app/subscription">查看套餐</Link></div></div>
      <div className={`account-route-state ${online > 0 ? 'is-online' : ''}`} aria-label={online > 0 ? 'Cloud 设备连接可用' : '当前没有在线设备'}><span><Smartphone size={21} />Muxvia App</span><i><b /></i><span>{online > 0 ? <Wifi size={21} /> : <WifiOff size={21} />}{online > 0 ? '连接可用' : '等待设备'}</span><small>{currentPlan?.name ?? subscription?.planId ?? '尚未订阅'}</small></div>
    </section>
    <section className="user-summary-strip">
      <Link to="/app/devices"><MonitorSmartphone size={20} /><span>在线设备</span><strong>{online}/{total}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/subscription"><CreditCard size={20} /><span>当前套餐</span><strong>{currentPlan?.name ?? subscription?.planId ?? '-'}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/usage"><Activity size={20} /><span>Relay 用量</span><strong>{bytes(usage?.relayTotalBytes ?? 0n)}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/orders"><ReceiptText size={20} /><span>待支付订单</span><strong>{pending}</strong><ArrowRight size={16} /></Link>
    </section>
    <div className="user-overview-grid">
      <section className="plain-section"><header><div><h2>设备状态</h2><p>在线位置由当前 Edge 实时上报</p></div><Link to="/app/devices">全部设备</Link></header><div className="device-rows">{!daemons.data?.daemons.length ? <div className="empty-inline"><p>还没有设备。</p><Link to="/app/devices">生成一次性安装命令<ArrowRight size={15} /></Link></div> : daemons.data.daemons.slice(0, 4).map((value) => <div key={value.daemon?.daemonId}><span><strong>{value.daemon?.displayName}</strong><small>{value.runtime?.online ? `${value.runtime.edgeName} · ${value.runtime.edgeRegion}` : '当前离线'}</small></span><Status active={Boolean(value.runtime?.online)}>{value.runtime?.online ? '在线' : value.daemon?.revoked ? '已撤销' : '离线'}</Status></div>)}</div></section>
      <section className="plain-section"><header><div><h2>本期 Cloud 权益</h2><p>{subscription ? `${dateTime(subscription.periodStart)} 至 ${dateTime(subscription.periodEnd)}` : '暂无生效订阅'}</p></div>{subscription && <Status active>{subscriptionState(subscription.state)}</Status>}</header><dl className="quota-list"><div><dt>可接入设备</dt><dd>{total} / {commerce.data?.entitlement?.capability?.cloudDaemonLimit ?? 0}</dd></div><div><dt>P2P 并发</dt><dd>{commerce.data?.entitlement?.capability?.managedP2pMaxConcurrency ?? 0}</dd></div><div><dt>Relay 剩余</dt><dd>{bytes(commerce.data?.entitlement?.relayRemainingBytes ?? 0n)}</dd></div></dl><Link className="text-action" to="/app/subscription">管理订阅<ArrowRight size={15} /></Link></section>
    </div>
  </>
}
