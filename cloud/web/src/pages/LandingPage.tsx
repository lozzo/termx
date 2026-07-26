import { ArrowRight, Check, Cloud, GitFork, Network, ShieldCheck, TerminalSquare } from 'lucide-react'
import { Link } from 'react-router'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'
import { money } from '../format'
import { ListPlansResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'

export function LandingPage() {
  const plans = useProtoQuery(['public', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 5 * 60_000)
  return <div className="landing-page">
    <header className="landing-header"><Link className="landing-brand" to="/"><img src={logo} alt="Muxvia" /><strong>Muxvia Cloud</strong></Link><nav aria-label="公开导航"><a href="#plans">套餐</a><a href="#security">安全</a><a href="https://github.com/muxvia/muxvia" aria-label="GitHub"><GitFork size={19} /></a><Link className="button button-quiet" to="/login">登录</Link><Link className="button button-primary" to="/register">创建账号</Link></nav></header>
    <main>
      <section className="landing-hero">
        <div className="hero-copy"><span className="eyebrow">MUXVIA CLOUD</span><h1>Muxvia Cloud</h1><p>在手机、TUI 和桌面客户端之间发现并连接你的 daemon。优先建立 P2P，必要时由 Edge 提供 Relay。</p><div className="hero-actions"><Link className="button button-primary" to="/register">开始使用<ArrowRight size={18} /></Link><Link className="button" to="/login">进入 Cloud</Link></div><dl><div><dt>连接数据</dt><dd>端到端加密</dd></div><div><dt>Cloud 路径</dt><dd>P2P / Relay</dd></div><div><dt>基础路径</dt><dd>Direct / SSH</dd></div></dl></div>
        <div className="hero-product" aria-label="Muxvia Cloud 产品连接画面">
          <div className="route-stage"><span><TerminalSquare size={20} />Android</span><i data-state="active" /><b>Cloud Edge · CN1</b><i /><span><Network size={20} />macOS daemon</span></div>
          <div className="terminal-window"><header><span>muxvia · zsh</span><small>Cloud P2P</small></header><pre><code><em>$</em> muxvia daemon status{`\n`}daemon     running{`\n`}cloud      connected{`\n`}edge       muxvia-cn1.omscd.com{`\n\n`}<em>$</em> echo muxvia-cloud-ok{`\n`}muxvia-cloud-ok</code></pre><footer><span><i /> 42 ms</span><span>端到端会话</span></footer></div>
        </div>
      </section>
      <section className="product-band"><div className="section-inner"><header><span className="eyebrow">连接路径</span><h2>Cloud 只协调连接，不进入终端数据面。</h2></header><div className="product-flow"><article><Cloud size={22} /><h3>Controller</h3><p>账号、订阅、设备目录、票据与用量结算。</p></article><article><Network size={22} /><h3>Edge</h3><p>信令、打洞和必要时的 TURN Relay。</p></article><article><TerminalSquare size={22} /><h3>daemon</h3><p>终端、文件和 CapabilityGrant 的唯一真值。</p></article></div></div></section>
      <section className="plans-band" id="plans"><div className="section-inner"><header><span className="eyebrow">套餐</span><h2>按你的设备规模选择 Cloud 能力。</h2><p>Direct 与 SSH 不依赖订阅，始终可用。</p></header><div className="public-plans">{plans.data?.plans.map((plan) => <article key={`${plan.planId}:${plan.version}`}><div><h3>{plan.name}</h3><p>{plan.description}</p></div><strong>{plan.monthlyPrice?.minorUnits === 0n ? '免费' : money(plan.monthlyPrice?.currency ?? 'CNY', plan.monthlyPrice?.minorUnits ?? 0n)}<small>{plan.monthlyPrice?.minorUnits === 0n ? '' : '/ 月'}</small></strong><ul><li><Check size={16} />{plan.capability?.cloudDaemonLimit ?? 0} 台 daemon</li><li><Check size={16} />{plan.capability?.managedP2pMaxConcurrency ?? 0} 路 P2P 并发</li><li><Check size={16} />{plan.capability?.relayMaxConcurrency ?? 0} 路 Relay 并发</li></ul><Link className="button" to="/register">选择{plan.name}<ArrowRight size={16} /></Link></article>)}</div></div></section>
      <section className="security-band" id="security"><div className="section-inner security-layout"><div><span className="eyebrow">安全边界</span><h2>你的终端内容不会交给 Controller。</h2></div><div><p><ShieldCheck size={20} />DeviceIdentity 私钥留在设备上，Cloud 只签发短期票据。</p><p><ShieldCheck size={20} />DTLS DataChannel 内验证 CapabilityGrant，Edge 无权决定终端权限。</p><p><ShieldCheck size={20} />Controller 或 Edge 暂时不可用，不会关闭仍在租约内的既有会话。</p></div></div></section>
      <section className="final-cta"><div><img src={logo} alt="" /><h2>把你的下一台设备接入 Muxvia Cloud。</h2><Link className="button button-primary" to="/register">创建账号<ArrowRight size={18} /></Link></div></section>
    </main>
    <footer className="landing-footer"><span>Muxvia Cloud · Development</span><a href="https://github.com/muxvia/muxvia">GitHub</a></footer>
  </div>
}
