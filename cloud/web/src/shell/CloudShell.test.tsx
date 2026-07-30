import { describe, expect, it } from 'vitest'
import { adminNavigationGroups } from './CloudShell'

describe('CloudShell admin navigation', () => {
  it('retains the capability set under the three required headings', () => {
    expect(adminNavigationGroups.map((group) => group.heading)).toEqual(['Infrastructure', 'Account operations', 'Governance'])
    expect(adminNavigationGroups.flatMap((group) => group.items.map(({ to, label }) => ({ to, label })))).toEqual([
      { to: '/app/admin/overview', label: '运营总览' },
      { to: '/app/admin/edges', label: 'Edge 管理' },
      { to: '/app/admin/daemons', label: '在线 daemon' },
      { to: '/app/admin/connections', label: '实时连接' },
      { to: '/app/admin/accounts', label: '用户与权限' },
      { to: '/app/admin/plans', label: '套餐' },
      { to: '/app/admin/subscriptions', label: '订阅' },
      { to: '/app/admin/orders', label: '订单与交易' },
      { to: '/app/admin/usage', label: '用量与结算' },
      { to: '/app/admin/certificates', label: '证书' },
      { to: '/app/admin/audit', label: '审计' },
      { to: '/app/admin/system', label: '系统' },
    ])
  })
})
