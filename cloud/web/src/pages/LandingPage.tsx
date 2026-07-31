import { ArrowRight, Check, Download, GitFork, LockKeyhole, MonitorSmartphone, QrCode, ShieldCheck, Smartphone, TerminalSquare } from 'lucide-react'
import { useLayoutEffect, useRef } from 'react'
import { Link, useLocation } from 'react-router'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'
import { money } from '../format'
import { ListPlansResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { Empty, Notice, Skeleton } from '../ui'

export function LandingPage() {
  const location = useLocation()
  const mainRef = useRef<HTMLElement>(null)
  const plans = useProtoQuery(['public', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 5 * 60_000)
  useLayoutEffect(() => {
    document.title = 'AnyTTY Cloud'
    mainRef.current?.focus({ preventScroll: true })
  }, [location.pathname])
  return <div className="landing-page">
    <header className="landing-header"><Link className="landing-brand" to="/"><img src={logo} alt="AnyTTY" /><strong>AnyTTY Cloud</strong></Link><nav aria-label="公开导航"><a href="#connect">如何连接</a><a href="#plans">套餐</a><a href="#security">安全</a><a href="https://github.com/anytty/anytty" aria-label="在 GitHub 查看 AnyTTY"><GitFork size={19} /></a><Link className="button button-primary" to="/login">登录 Cloud</Link></nav></header>
    <main id="landing-main-content" ref={mainRef} tabIndex={-1} aria-label="AnyTTY Cloud 公开首页">
      <section className="landing-hero">
        <div className="hero-copy"><span className="eyebrow">ANYTTY CLOUD</span><h1>AnyTTY Cloud</h1><strong className="hero-statement">随时回到你的电脑。</strong><p>Cloud 账号用于 daemon 注册、连接路由、Relay、订阅与管理。AnyTTY App 不需要账号；扫描目标服务生成的配对二维码后，端点只保存在当前设备。</p><div className="hero-actions"><Link className="button button-primary" to="/login">登录 Cloud 控制台<ArrowRight size={18} /></Link></div><dl><div><dt>App 配对</dt><dd>目标服务二维码</dd></div><div><dt>连接方式</dt><dd>P2P / Relay</dd></div><div><dt>本地保存</dt><dd>无账号同步</dd></div></dl></div>
        <div className="hero-product" aria-label="扫码配对后的 AnyTTY Cloud 连接路径">
          <div className="route-stage"><span><Smartphone size={20} />已配对 App</span><i data-state="active" /><b>Cloud P2P / Relay</b><i /><span><MonitorSmartphone size={20} />已注册 daemon</span></div>
          <div className="terminal-window"><header><span>开发 Mac · zsh</span><small>Cloud P2P</small></header><pre><code><em>$</em> anytty daemon status{`\n`}daemon     running{`\n`}cloud      connected{`\n`}edge       cn1.edge.anytty.com{`\n\n`}<em>$</em> echo anytty-cloud-ok{`\n`}anytty-cloud-ok</code></pre><footer><span><i /> 在线 · 42 ms</span><span><LockKeyhole size={13} />端到端会话</span></footer></div>
        </div>
      </section>
      <section className="product-band" id="connect"><div className="section-inner"><header><span className="eyebrow">三步开始</span><h2>Cloud 管理 daemon，App 只接受扫码配对。</h2><p>Cloud 账号不会填充 App 设备列表。每台手机都必须扫描目标 daemon 或服务生成的配对二维码。</p></header><div className="product-flow"><article><span className="step-number">01</span><Download size={22} /><h3>注册 daemon</h3><p>登录 Cloud 控制台生成一次性命令，并在需要远程访问的电脑上运行。</p></article><article><span className="step-number">02</span><QrCode size={22} /><h3>扫描服务二维码</h3><p>在目标 daemon 或服务上生成配对二维码，再用无需账号的 AnyTTY App 扫描。</p></article><article><span className="step-number">03</span><TerminalSquare size={22} /><h3>从本地列表连接</h3><p>App 在本机保存已配对端点；连接优先 P2P，必要时使用 Cloud Relay。</p></article></div></div></section>
      <section className="plans-band" id="plans"><div className="section-inner"><header><span className="eyebrow">套餐</span><h2>先免费使用，需要时再增加容量。</h2><p>Direct 与 SSH 不依赖 Cloud 订阅；套餐用于 daemon 注册、Cloud 路由、P2P 与 Relay，不提供 App 账号同步。</p></header><div className="public-plans">{plans.isPending ? <div className="public-plans-state"><Skeleton rows={3} /></div> : plans.error ? <div className="public-plans-state"><Notice tone="error">暂时无法读取套餐，请稍后刷新页面。</Notice></div> : !plans.data?.plans.length ? <div className="public-plans-state"><Empty>当前没有可购买的套餐。</Empty></div> : plans.data.plans.map((plan) => <article key={`${plan.planId}:${plan.version}`}><div><h3>{plan.name}</h3><p>{plan.description}</p></div><strong>{plan.monthlyPrice?.minorUnits === 0n ? '免费' : money(plan.monthlyPrice?.currency ?? 'CNY', plan.monthlyPrice?.minorUnits ?? 0n)}<small>{plan.monthlyPrice?.minorUnits === 0n ? '' : '/ 月'}</small></strong><ul><li><Check size={16} />{plan.capability?.cloudDaemonLimit ?? 0} 个 daemon</li><li><Check size={16} />{plan.capability?.managedP2pMaxConcurrency ?? 0} 路 P2P 并发</li><li><Check size={16} />{plan.capability?.relayMaxConcurrency ?? 0} 路 Relay 并发</li></ul><Link className="button" to="/login">登录使用{plan.name}<ArrowRight size={16} /></Link></article>)}</div></div></section>
      <section className="security-band" id="security"><div className="section-inner security-layout"><div><span className="eyebrow">安全边界</span><h2>Cloud 管理服务路由，但不能把设备加入 App。</h2></div><div><p><ShieldCheck size={20} />App 配对凭据和端点列表保留在本机，不属于 Cloud 账号数据。</p><p><ShieldCheck size={20} />终端权限由目标设备在加密连接内验证，Edge 无法读取或提升权限。</p><p><ShieldCheck size={20} />Cloud 账号只管理 daemon 注册、路由、Relay、订阅与运营配置。</p></div></div></section>
      <section className="final-cta"><div><img src={logo} alt="" /><h2>先注册 daemon，再让 App 扫描目标服务二维码。</h2><p>登录 Cloud 账号管理服务接入；AnyTTY App 本身无需登录。</p><Link className="button button-primary" to="/login">登录 Cloud 控制台<ArrowRight size={18} /></Link></div></section>
    </main>
    <footer className="landing-footer"><span>AnyTTY Cloud · Development</span><a href="https://github.com/anytty/anytty">GitHub</a></footer>
  </div>
}
