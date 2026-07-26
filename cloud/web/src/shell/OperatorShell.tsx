import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Activity, BadgeCent, BookOpenCheck, Boxes, Cable, ChevronLeft, CircleDollarSign, Gauge, KeyRound, LogOut, Menu, Network, Search, Server, Settings, ShieldCheck, Users, X } from 'lucide-react'
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router'
import { APIError, protoSend } from '../api'
import { GetCurrentAccountResponseSchema, LogoutAccountSessionRequestSchema, LogoutAccountSessionResponseSchema, VerifyRecentAuthenticationRequestSchema, VerifyRecentAuthenticationResponseSchema } from '../generated/cloud/v1/account_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Field, Input, Skeleton } from '../ui'

const navigation = [
  { to: '/overview', label: '总览', icon: Gauge },
  { to: '/edges', label: 'Edge 管理', icon: Server },
  { to: '/daemons', label: '在线 daemon', icon: Network },
  { to: '/connections', label: '实时连接', icon: Cable },
  { to: '/accounts', label: '用户与权限', icon: Users },
  { to: '/plans', label: '套餐', icon: Boxes },
  { to: '/subscriptions', label: '订阅', icon: ShieldCheck },
  { to: '/orders', label: '订单与交易', icon: CircleDollarSign },
  { to: '/certificates', label: '证书', icon: KeyRound },
  { to: '/usage', label: '用量与结算', icon: Activity },
  { to: '/audit', label: '审计', icon: BookOpenCheck },
  { to: '/system', label: '系统', icon: Settings },
] as const

const titles = new Map(navigation.map((item) => [item.to, item.label]))

export function OperatorShell() {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [collapsed, setCollapsed] = useState(false)
  const [drawer, setDrawer] = useState(false)
  const [search, setSearch] = useState('')
  const [reauth, setReauth] = useState(false)
  const [password, setPassword] = useState('')
  const current = useProtoQuery(['account', 'current'], '/api/account/current', GetCurrentAccountResponseSchema, 60_000)

  useEffect(() => {
    if (current.error instanceof APIError && current.error.status === 401) navigate('/login', { replace: true })
  }, [current.error, navigate])

  useEffect(() => {
    const source = new EventSource('/api/operator/events')
    source.addEventListener('runtime', (event) => {
      const payload = JSON.parse((event as MessageEvent<string>).data) as { resource_kind?: string }
      if (payload.resource_kind === 'edge') {
        void queryClient.invalidateQueries({ queryKey: ['edges'] })
        void queryClient.invalidateQueries({ queryKey: ['daemons'] })
        void queryClient.invalidateQueries({ queryKey: ['connections'] })
      }
      if (payload.resource_kind === 'daemon') void queryClient.invalidateQueries({ queryKey: ['daemons'] })
      if (payload.resource_kind === 'session' || payload.resource_kind === 'allocation') void queryClient.invalidateQueries({ queryKey: ['connections'] })
      void queryClient.invalidateQueries({ queryKey: ['overview'] })
    })
    return () => source.close()
  }, [queryClient])

  const logout = useMutation({ mutationFn: () => protoSend('/api/account/logout', LogoutAccountSessionRequestSchema, create(LogoutAccountSessionRequestSchema), LogoutAccountSessionResponseSchema), onSuccess: () => { queryClient.clear(); navigate('/login', { replace: true }) } })
  const verify = useMutation({ mutationFn: () => protoSend('/api/account/recent-auth', VerifyRecentAuthenticationRequestSchema, create(VerifyRecentAuthenticationRequestSchema, { password }), VerifyRecentAuthenticationResponseSchema), onSuccess: () => { setPassword(''); setReauth(false); void queryClient.invalidateQueries({ queryKey: ['account', 'current'] }) } })
  const title = useMemo(() => {
    const root = `/${location.pathname.split('/')[1]}`
    return titles.get(root as typeof navigation[number]['to']) ?? 'Muxvia Cloud'
  }, [location.pathname])

  if (current.isPending) return <div className="boot-shell"><Skeleton rows={8} /></div>
  if (!current.data) return <Navigate to="/login" replace />

  function submitSearch(event: FormEvent) { event.preventDefault(); const value = search.trim(); if (value) navigate(`/accounts?query=${encodeURIComponent(value)}`) }

  return <div className={`operator-layout ${collapsed ? 'nav-collapsed' : ''}`}>
    <aside className={`sidebar ${drawer ? 'sidebar-open' : ''}`}>
      <div className="brand-row"><div className="brand-mark">M</div><div className="brand-copy"><strong>Muxvia</strong><span>Cloud 运营台</span></div><button className="mobile-close" aria-label="关闭导航" onClick={() => setDrawer(false)}><X size={20} /></button></div>
      <div className="control-rail"><span className="pulse-dot" /><div><strong>Controller 在线</strong><span>开发环境</span></div></div>
      <nav aria-label="运营模块">{navigation.map(({ to, label, icon: Icon }) => <NavLink key={to} to={to} onClick={() => setDrawer(false)} title={collapsed ? label : undefined}><Icon size={18} /><span>{label}</span></NavLink>)}</nav>
      <button className="collapse-button" onClick={() => setCollapsed((value) => !value)} title={collapsed ? '展开导航' : '折叠导航'}><ChevronLeft size={18} /><span>折叠导航</span></button>
    </aside>
    {drawer && <button className="drawer-scrim" aria-label="关闭导航" onClick={() => setDrawer(false)} />}
    <div className="workspace">
      <header className="topbar">
        <button className="menu-button" aria-label="打开导航" onClick={() => setDrawer(true)}><Menu size={20} /></button>
        <div className="module-title"><span>运营后台</span><strong>{title}</strong></div>
        <form className="global-search" onSubmit={submitSearch}><Search size={17} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索账号名称、邮箱或 ID" /></form>
        <div className="topbar-account"><button onClick={() => setReauth(true)} title="重新验证身份"><BadgeCent size={17} /><span>{current.data.account?.displayName}</span></button><button className="logout-button" onClick={() => logout.mutate()} title="退出登录"><LogOut size={18} /></button></div>
      </header>
      <main className="content"><Outlet /></main>
    </div>
    <Dialog title="重新验证身份" open={reauth} onClose={() => setReauth(false)} footer={<><Button tone="quiet" onClick={() => setReauth(false)}>取消</Button><Button tone="primary" onClick={() => verify.mutate()} disabled={!password || verify.isPending}>确认</Button></>}>
      <Field label="当前密码"><Input type="password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} /></Field>{verify.error && <p className="form-error">{verify.error.message}</p>}
    </Dialog>
  </div>
}
