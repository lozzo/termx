import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Activity, BookOpenCheck, Boxes, Cable, ChevronLeft, CircleDollarSign, CreditCard, Gauge, KeyRound, LogOut, Menu, MonitorSmartphone, Network, ReceiptText, Server, Settings, ShieldCheck, UserRound, Users, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { NavLink, Navigate, Outlet, useLocation, useNavigate, useOutletContext } from 'react-router'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'
import { APIError, protoSend } from '../api'
import { AccountRole, GetCurrentAccountResponseSchema, LogoutAccountSessionRequestSchema, LogoutAccountSessionResponseSchema, type GetCurrentAccountResponse } from '../generated/cloud/v1/account_pb'
import { useProtoQuery } from '../query'
import { IconButton, Skeleton } from '../ui'

const userNavigation = [
  { to: '/app/overview', label: '概览', icon: Gauge },
  { to: '/app/devices', label: '我的设备', icon: MonitorSmartphone },
  { to: '/app/subscription', label: '订阅套餐', icon: CreditCard },
  { to: '/app/orders', label: '我的订单', icon: ReceiptText },
  { to: '/app/usage', label: '用量', icon: Activity },
  { to: '/app/security', label: '账号安全', icon: UserRound },
] as const

const adminNavigation = [
  { to: '/app/admin/overview', label: '运营总览', icon: Gauge },
  { to: '/app/admin/edges', label: 'Edge 管理', icon: Server },
  { to: '/app/admin/daemons', label: '在线 daemon', icon: Network },
  { to: '/app/admin/connections', label: '实时连接', icon: Cable },
  { to: '/app/admin/accounts', label: '用户与权限', icon: Users },
  { to: '/app/admin/plans', label: '套餐', icon: Boxes },
  { to: '/app/admin/subscriptions', label: '订阅', icon: ShieldCheck },
  { to: '/app/admin/orders', label: '订单与交易', icon: CircleDollarSign },
  { to: '/app/admin/certificates', label: '证书', icon: KeyRound },
  { to: '/app/admin/usage', label: '用量与结算', icon: Activity },
  { to: '/app/admin/audit', label: '审计', icon: BookOpenCheck },
  { to: '/app/admin/system', label: '系统', icon: Settings },
] as const

type ShellContext = { current: GetCurrentAccountResponse; isOperator: boolean }

export function CloudShell() {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [collapsed, setCollapsed] = useState(false)
  const [drawer, setDrawer] = useState(false)
  const current = useProtoQuery(['account', 'current'], '/api/account/current', GetCurrentAccountResponseSchema, 60_000)
  const isOperator = Boolean(current.data?.roles.some((value) => value === AccountRole.OPERATOR || value === AccountRole.ADMIN))

  useEffect(() => {
    if (current.error instanceof APIError && current.error.status === 401) navigate('/login', { replace: true, state: { from: location.pathname } })
  }, [current.error, location.pathname, navigate])

  useEffect(() => {
    if (!isOperator) return
    const source = new EventSource('/api/operator/events')
    source.addEventListener('runtime', (event) => {
      const payload = JSON.parse((event as MessageEvent<string>).data) as { resource_kind?: string }
      if (payload.resource_kind === 'edge') void queryClient.invalidateQueries({ queryKey: ['edges'] })
      if (payload.resource_kind === 'daemon') {
        void queryClient.invalidateQueries({ queryKey: ['daemons'] })
        void queryClient.invalidateQueries({ queryKey: ['user', 'daemons'] })
      }
      if (payload.resource_kind === 'session' || payload.resource_kind === 'allocation') void queryClient.invalidateQueries({ queryKey: ['connections'] })
      void queryClient.invalidateQueries({ queryKey: ['overview'] })
    })
    return () => source.close()
  }, [isOperator, queryClient])

  const logout = useMutation({
    mutationFn: () => protoSend('/api/account/logout', LogoutAccountSessionRequestSchema, create(LogoutAccountSessionRequestSchema), LogoutAccountSessionResponseSchema),
    onSuccess: () => { queryClient.clear(); navigate('/login', { replace: true }) },
  })
  const title = useMemo(() => [...userNavigation, ...adminNavigation].find((item) => location.pathname.startsWith(item.to))?.label ?? 'Muxvia Cloud', [location.pathname])

  if (current.isPending) return <div className="boot-shell"><img src={logo} alt="" /><Skeleton rows={6} /></div>
  if (!current.data?.account) return <Navigate to="/login" replace />

  const navigation = (items: readonly typeof userNavigation[number][] | readonly typeof adminNavigation[number][], label: string) => <nav aria-label={label}>
    {items.map(({ to, label: itemLabel, icon: Icon }) => <NavLink key={to} to={to} onClick={() => setDrawer(false)} title={collapsed ? itemLabel : undefined}><Icon size={18} /><span>{itemLabel}</span></NavLink>)}
  </nav>

  return <div className={`cloud-layout ${collapsed ? 'nav-collapsed' : ''}`}>
    <a className="skip-link" href="#main-content">跳到主要内容</a>
    <aside className={`sidebar ${drawer ? 'sidebar-open' : ''}`}>
      <div className="brand-row"><img src={logo} alt="Muxvia" /><div className="brand-copy"><strong>Muxvia</strong><span>Cloud</span></div><IconButton className="mobile-close" label="关闭导航" onClick={() => setDrawer(false)}><X size={20} /></IconButton></div>
      <div className="nav-section"><span className="nav-caption">我的 Cloud</span>{navigation(userNavigation, '用户功能')}</div>
      {isOperator && <div className="nav-section nav-section-admin"><span className="nav-caption">运营管理</span>{navigation(adminNavigation, '运营管理')}</div>}
      <button className="collapse-button" onClick={() => setCollapsed((value) => !value)} title={collapsed ? '展开导航' : '折叠导航'}><ChevronLeft size={18} /><span>折叠导航</span></button>
    </aside>
    {drawer && <button className="drawer-scrim" aria-label="关闭导航" onClick={() => setDrawer(false)} />}
    <div className="workspace">
      <header className="topbar">
        <IconButton className="menu-button" label="打开导航" onClick={() => setDrawer(true)}><Menu size={20} /></IconButton>
        <div className="module-title"><span>{location.pathname.startsWith('/app/admin') ? '运营管理' : '我的 Cloud'}</span><strong>{title}</strong></div>
        <div className="topbar-account"><NavLink to="/app/security"><b aria-hidden="true">{current.data.account.displayName.trim().slice(0, 1).toUpperCase()}</b><span><strong>{current.data.account.displayName}</strong><small>{isOperator ? '管理员 · ' : ''}{current.data.account.email}</small></span></NavLink><IconButton label="退出登录" onClick={() => logout.mutate()} disabled={logout.isPending}><LogOut size={18} /></IconButton></div>
      </header>
      <main className={`content ${location.pathname.startsWith('/app/admin') ? 'content-admin' : 'content-user'}`} id="main-content" tabIndex={-1}><Outlet context={{ current: current.data, isOperator } satisfies ShellContext} /></main>
      <nav className="mobile-bottom-nav" aria-label="手机主导航">{userNavigation.filter((item) => item.to !== '/app/orders').map(({ to, label, icon: Icon }) => <NavLink key={to} to={to}><Icon size={20} /><span>{label}</span></NavLink>)}</nav>
    </div>
  </div>
}

export function AdminGuard() {
  const context = useOutletContext<ShellContext>()
  return context.isOperator ? <Outlet context={context} /> : <Navigate to="/app/no-permission" replace />
}

export function useCloudAccount() { return useOutletContext<ShellContext>() }
