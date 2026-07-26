import { ArrowRight, Check, Download, GitFork, LockKeyhole, MonitorSmartphone, ShieldCheck, Smartphone, TerminalSquare } from 'lucide-react'
import { Link } from 'react-router'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'
import { money } from '../format'
import { ListPlansResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { Empty, Notice, Skeleton } from '../ui'

export function LandingPage() {
  const plans = useProtoQuery(['public', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 5 * 60_000)
  return <div className="landing-page">
    <header className="landing-header"><Link className="landing-brand" to="/"><img src={logo} alt="Muxvia" /><strong>Muxvia Cloud</strong></Link><nav aria-label="公开导航"><a href="#connect">如何连接</a><a href="#plans">套餐</a><a href="#security">安全</a><a href="https://github.com/muxvia/muxvia" aria-label="在 GitHub 查看 Muxvia"><GitFork size={19} /></a><Link className="button button-quiet" to="/login">登录</Link><Link className="button button-primary" to="/register">免费开始</Link></nav></header>
    <main>
      <section className="landing-hero">
        <div className="hero-copy"><span className="eyebrow">MUXVIA CLOUD</span><h1>Muxvia Cloud</h1><strong className="hero-statement">随时回到你的电脑。</strong><p>在手机、TUI 和桌面客户端中找到自己的设备并安全连接。Cloud 优先直连，网络受限时自动通过 Edge 中转。</p><div className="hero-actions"><Link className="button button-primary" to="/register">免费开始<ArrowRight size={18} /></Link><Link className="button" to="/login">登录账号</Link></div><dl><div><dt>你的数据</dt><dd>端到端加密</dd></div><div><dt>连接方式</dt><dd>P2P / Relay</dd></div><div><dt>无需订阅</dt><dd>Direct / SSH</dd></div></dl></div>
        <div className="hero-product" aria-label="Muxvia Cloud 产品连接画面">
          <div className="route-stage"><span><Smartphone size={20} />Muxvia App</span><i data-state="active" /><b>Cloud Edge · CN1</b><i /><span><MonitorSmartphone size={20} />我的 Mac</span></div>
          <div className="terminal-window"><header><span>开发 Mac · zsh</span><small>Cloud P2P</small></header><pre><code><em>$</em> muxvia daemon status{`\n`}daemon     running{`\n`}cloud      connected{`\n`}edge       muxvia-cn1.omscd.com{`\n\n`}<em>$</em> echo muxvia-cloud-ok{`\n`}muxvia-cloud-ok</code></pre><footer><span><i /> 在线 · 42 ms</span><span><LockKeyhole size={13} />端到端会话</span></footer></div>
        </div>
      </section>
      <section className="product-band" id="connect"><div className="section-inner"><header><span className="eyebrow">三步开始</span><h2>把一台电脑放进你的设备列表。</h2><p>Cloud 负责发现和连接，终端与文件仍然只在你的设备之间传输。</p></header><div className="product-flow"><article><span className="step-number">01</span><Download size={22} /><h3>安装 daemon</h3><p>登录后生成一条一次性命令，在需要远程访问的电脑上运行。</p></article><article><span className="step-number">02</span><Smartphone size={22} /><h3>登录 Muxvia App</h3><p>同一账号会看到已经接入的设备，无需手工填写服务器和密钥。</p></article><article><span className="step-number">03</span><TerminalSquare size={22} /><h3>打开终端</h3><p>优先建立 P2P；直连不可用时由 Edge Relay 保持可达。</p></article></div></div></section>
      <section className="plans-band" id="plans"><div className="section-inner"><header><span className="eyebrow">套餐</span><h2>先免费使用，需要时再增加容量。</h2><p>Direct 与 SSH 不依赖 Cloud 订阅；套餐只为托管发现、P2P 与 Relay 买单。</p></header><div className="public-plans">{plans.isPending ? <div className="public-plans-state"><Skeleton rows={3} /></div> : plans.error ? <div className="public-plans-state"><Notice tone="error">暂时无法读取套餐，请稍后刷新页面。</Notice></div> : !plans.data?.plans.length ? <div className="public-plans-state"><Empty>当前没有可购买的套餐。</Empty></div> : plans.data.plans.map((plan) => <article key={`${plan.planId}:${plan.version}`}><div><h3>{plan.name}</h3><p>{plan.description}</p></div><strong>{plan.monthlyPrice?.minorUnits === 0n ? '免费' : money(plan.monthlyPrice?.currency ?? 'CNY', plan.monthlyPrice?.minorUnits ?? 0n)}<small>{plan.monthlyPrice?.minorUnits === 0n ? '' : '/ 月'}</small></strong><ul><li><Check size={16} />{plan.capability?.cloudDaemonLimit ?? 0} 台设备</li><li><Check size={16} />{plan.capability?.managedP2pMaxConcurrency ?? 0} 路 P2P 并发</li><li><Check size={16} />{plan.capability?.relayMaxConcurrency ?? 0} 路 Relay 并发</li></ul><Link className="button" to="/register">使用{plan.name}<ArrowRight size={16} /></Link></article>)}</div></div></section>
      <section className="security-band" id="security"><div className="section-inner security-layout"><div><span className="eyebrow">安全边界</span><h2>Cloud 能帮你找到设备，但看不到你的终端。</h2></div><div><p><ShieldCheck size={20} />设备私钥保留在本机，Cloud 只签发短期连接票据。</p><p><ShieldCheck size={20} />终端权限由目标设备在加密连接内验证，Edge 无法读取或提升权限。</p><p><ShieldCheck size={20} />Controller 或 Edge 暂时不可用，不会中断仍在有效期内的既有会话。</p></div></div></section>
      <section className="final-cta"><div><img src={logo} alt="" /><h2>从第一台设备开始使用 Muxvia Cloud。</h2><p>注册账号后生成一次性安装命令，几分钟内完成接入。</p><Link className="button button-primary" to="/register">免费创建账号<ArrowRight size={18} /></Link></div></section>
    </main>
    <footer className="landing-footer"><span>Muxvia Cloud · Development</span><a href="https://github.com/muxvia/muxvia">GitHub</a></footer>
  </div>
}
