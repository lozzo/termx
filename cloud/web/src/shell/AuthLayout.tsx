import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Check, LockKeyhole, MonitorSmartphone, Network, ShieldCheck, Smartphone } from 'lucide-react'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'

export function AuthLayout({ title, description, alternate, children }: { title: string; description: string; alternate: ReactNode; children: ReactNode }) {
  return <main className="auth-page">
    <section className="auth-panel">
      <Link className="auth-brand" to="/"><img src={logo} alt="AnyTTY" /><span><strong>AnyTTY Cloud</strong><small>随时回到你的电脑</small></span></Link>
      <div className="auth-form-wrap"><h1>{title}</h1><p>{description}</p>{children}<div className="auth-alternate">{alternate}</div></div>
      <footer><span>Development</span><Link to="/">返回首页</Link></footer>
    </section>
    <aside className="auth-scene" aria-label="AnyTTY Cloud 连接能力">
      <div className="auth-scene-copy"><span className="eyebrow">一个账号，所有设备</span><h2>登录后，从手机直接找到自己的电脑。</h2><div className="auth-route" aria-label="AnyTTY Cloud 加密连接路径"><span><Smartphone size={20} />AnyTTY App</span><i /><b><Network size={18} />Cloud Edge</b><i /><span><MonitorSmartphone size={20} />我的设备</span></div><ul><li><Network size={18} />优先 P2P，必要时自动使用 Relay</li><li><ShieldCheck size={18} />Cloud 不解密终端与文件内容</li><li><Check size={18} />Direct 与 SSH 始终独立可用</li></ul><p className="auth-proof"><LockKeyhole size={16} />连接权限由你的设备最终确认</p></div>
    </aside>
  </main>
}
