import { Activity, ArrowRight, CreditCard, MonitorSmartphone, Plus, ReceiptText, Server, ShieldCheck, UserRound, WifiOff } from 'lucide-react'
import { Link } from 'react-router'
import { bytes, dateTime, subscriptionState } from '../format'
import { GetAccountCommerceResponseSchema, ListPlansResponseSchema, OrderStatus } from '../generated/cloud/v1/commerce_pb'
import { DaemonState, ListMyDaemonsResponseSchema } from '../generated/cloud/v1/enrollment_pb'
import { useProtoQuery } from '../query'
import { useCloudAccount } from '../shell/CloudShell'
import { ErrorState, PageHeader, Skeleton, Status } from '../ui'

export function UserOverviewPage() {
  const { current } = useCloudAccount()
  const commerce = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  const daemons = useProtoQuery(['user', 'daemons'], '/api/daemons', ListMyDaemonsResponseSchema, 8_000)
  const plans = useProtoQuery(['user', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 60_000)
  if (commerce.isPending || daemons.isPending || plans.isPending) return <><PageHeader title={`你好，${current.account?.displayName ?? ''}`} meta="正在读取你的 Cloud 状态" /><Skeleton rows={7} /></>
  const failedQuery = commerce.error ? commerce : daemons.error ? daemons : plans.error ? plans : undefined
  if (failedQuery) return <ErrorState error={failedQuery.error} onRetry={() => void failedQuery.refetch()} />
  const online = daemons.data?.daemons.filter((value) => value.runtime?.online).length ?? 0
  const total = daemons.data?.daemons.length ?? 0
  const pending = commerce.data?.orders.filter((value) => value.status === OrderStatus.PENDING).length ?? 0
  const subscription = commerce.data?.subscription
  const usage = commerce.data?.usage
  const currentPlan = plans.data?.plans.find((value) => value.planId === subscription?.planId && value.version === subscription.planVersion)
  return <>
    <section className="account-hero">
      <div className="account-hero-copy"><span className="account-kicker"><ShieldCheck size={15} />AnyTTY Cloud 已就绪</span><h1>你好，{current.account?.displayName ?? ''}</h1><p>{total === 0 ? '先在 Cloud 注册 daemon，再在目标服务生成配对二维码供 App 扫描。Cloud 账号不会同步 App 设备列表。' : `${online} 个 daemon 在线。每台手机仍需扫描目标服务生成的二维码，配对记录只保存在 App 本机。`}</p><div className="account-hero-actions"><Link className="button button-primary" to="/app/devices">{total === 0 ? <><Plus size={17} />注册第一个 daemon</> : <><MonitorSmartphone size={17} />管理 daemon</>}</Link><Link className="button" to="/app/subscription">查看套餐</Link></div></div>
      <div className={`account-route-state ${online > 0 ? 'is-online' : ''}`} aria-label={online > 0 ? 'Cloud daemon 管理状态可用' : '当前没有在线 daemon'}><span><UserRound size={21} />Cloud 账号</span><i><b /></i><span>{online > 0 ? <Server size={21} /> : <WifiOff size={21} />}{online > 0 ? 'daemon 在线' : '等待 daemon'}</span><small>{currentPlan?.name ?? subscription?.planId ?? '尚未订阅'} · App 设备需扫码添加</small></div>
    </section>
    <section className="user-summary-strip">
      <Link to="/app/devices"><MonitorSmartphone size={20} /><span>在线 daemon</span><strong>{online}/{total}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/subscription"><CreditCard size={20} /><span>当前套餐</span><strong>{currentPlan?.name ?? subscription?.planId ?? '-'}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/usage"><Activity size={20} /><span>Relay 用量</span><strong>{bytes(usage?.relayTotalBytes ?? 0n)}</strong><ArrowRight size={16} /></Link>
      <Link to="/app/orders"><ReceiptText size={20} /><span>待支付订单</span><strong>{pending}</strong><ArrowRight size={16} /></Link>
    </section>
    <div className="user-overview-grid">
      <section className="plain-section"><header><div><h2>daemon 状态</h2><p>Cloud 只显示已注册服务及其 Edge 在线位置</p></div><Link to="/app/devices">全部 daemon</Link></header><div className="device-rows">{!daemons.data?.daemons.length ? <div className="empty-inline"><p>还没有注册 daemon。</p><Link to="/app/devices">生成一次性注册命令<ArrowRight size={15} /></Link></div> : daemons.data.daemons.slice(0, 4).map((value) => { const blocked = value.daemon?.state === DaemonState.BLOCKED; return <div key={value.daemon?.daemonId}><span><strong>{value.daemon?.displayName}</strong><small>{!blocked && value.runtime?.online ? `${value.runtime.edgeName} · ${value.runtime.edgeRegion}` : blocked ? 'Cloud 已停用' : '当前离线'}</small></span><Status active={!blocked && Boolean(value.runtime?.online)}>{blocked ? '已停用' : value.runtime?.online ? '在线' : '离线'}</Status></div> })}</div></section>
      <section className="plain-section"><header><div><h2>本期 Cloud 权益</h2><p>{subscription ? `${dateTime(subscription.periodStart)} 至 ${dateTime(subscription.periodEnd)}` : '暂无生效订阅'}</p></div>{subscription && <Status active>{subscriptionState(subscription.state)}</Status>}</header><dl className="quota-list"><div><dt>在线 daemon</dt><dd>{online} / {commerce.data?.entitlement?.capability?.cloudDaemonLimit ?? 0}</dd></div><div><dt>P2P</dt><dd>不限并发</dd></div><div><dt>Relay 剩余</dt><dd>{bytes(commerce.data?.entitlement?.relayRemainingBytes ?? 0n)}</dd></div></dl><Link className="text-action" to="/app/subscription">管理订阅<ArrowRight size={15} /></Link></section>
    </div>
  </>
}
