import { LockKeyhole } from 'lucide-react'
import { Link } from 'react-router'

export function ForbiddenPage() {
  return <section className="state-page"><LockKeyhole size={28} /><h1>没有运营管理权限</h1><p>当前账号可以继续使用设备、订阅和安全功能。</p><Link className="button button-primary" to="/app/overview">返回我的 Cloud</Link></section>
}
