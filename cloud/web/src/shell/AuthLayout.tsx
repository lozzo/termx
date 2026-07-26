import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Check, Network, ShieldCheck } from 'lucide-react'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'

export function AuthLayout({ title, description, alternate, children }: { title: string; description: string; alternate: ReactNode; children: ReactNode }) {
  return <main className="auth-page">
    <section className="auth-panel">
      <Link className="auth-brand" to="/"><img src={logo} alt="Muxvia" /><span><strong>Muxvia Cloud</strong><small>安全连接你的每一台设备</small></span></Link>
      <div className="auth-form-wrap"><h1>{title}</h1><p>{description}</p>{children}<div className="auth-alternate">{alternate}</div></div>
      <footer><span>Development</span><Link to="/">返回首页</Link></footer>
    </section>
    <aside className="auth-scene" aria-label="Muxvia Cloud 连接能力">
      <div className="auth-scene-copy"><span className="eyebrow">MUXVIA CLOUD</span><h2>连接由 Cloud 协调，终端数据始终端到端传输。</h2><ul><li><Network size={18} />P2P 优先，必要时使用 Relay</li><li><ShieldCheck size={18} />Controller 不解密终端内容</li><li><Check size={18} />Direct 与 SSH 始终独立可用</li></ul></div>
      <div className="signal-track" aria-hidden="true"><i /><i /><i /><span>DEVICE</span><b>EDGE</b><span>DAEMON</span></div>
    </aside>
  </main>
}
