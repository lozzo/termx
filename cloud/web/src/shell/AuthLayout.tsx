import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Check, LockKeyhole, MonitorSmartphone, Network, QrCode, ShieldCheck, UserRound } from 'lucide-react'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'

export function AuthLayout({ title, description, alternate, children }: { title: string; description: string; alternate: ReactNode; children: ReactNode }) {
  return <main className="auth-page">
    <section className="auth-panel">
      <Link className="auth-brand" to="/"><img src={logo} alt="AnyTTY" /><span><strong>AnyTTY Cloud</strong><small>随时回到你的电脑</small></span></Link>
      <div className="auth-form-wrap"><h1>{title}</h1><p>{description}</p>{children}<div className="auth-alternate">{alternate}</div></div>
      <footer><span>Development</span><Link to="/">返回首页</Link></footer>
    </section>
    <aside className="auth-scene" aria-label="AnyTTY Cloud 连接能力">
      <div className="auth-scene-copy"><span className="eyebrow">Cloud 账号管理服务</span><h2>注册 daemon 与订阅；App 设备仍需扫码添加。</h2><div className="auth-route" aria-label="AnyTTY Cloud daemon 注册与 App 配对边界"><span><UserRound size={20} />Cloud 账号</span><i /><b><Network size={18} />daemon 注册</b><i /><span><QrCode size={20} />目标服务 QR</span></div><ul><li><MonitorSmartphone size={18} />AnyTTY App 无账号，不会自动发现同账号设备</li><li><QrCode size={18} />每台手机扫描目标服务生成的配对二维码</li><li><ShieldCheck size={18} />Cloud 管理路由与 Relay，不解密终端和文件</li><li><Check size={18} />Direct 与 SSH 始终独立可用</li></ul><p className="auth-proof"><LockKeyhole size={16} />配对端点只保存在 App 本机</p></div>
    </aside>
  </main>
}
