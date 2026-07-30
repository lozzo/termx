import { Component, lazy, Suspense, useLayoutEffect, useRef, type ComponentType, type ReactNode } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation, useOutletContext } from 'react-router'
import { LoginPage } from './pages/LoginPage'
import { LandingPage } from './pages/LandingPage'
import { AdminGuard, CloudShell } from './shell/CloudShell'
import { Button, Notice, Skeleton } from './ui'

type RouteModule = { default: ComponentType }
type RouteModuleLoader = () => Promise<RouteModule>

class RouteResourceLoadError extends Error {
  readonly cause: unknown

  constructor(cause: unknown) {
    super('Cloud route resource failed to load')
    this.name = 'RouteResourceLoadError'
    this.cause = cause
  }
}

export function createLazyRoute(loader: RouteModuleLoader): ComponentType {
  return lazy(async () => {
    try {
      return await loader()
    } catch (error) {
      throw new RouteResourceLoadError(error)
    }
  })
}

type RouteResourceBoundaryProps = { children: ReactNode }
type RouteResourceBoundaryState = { error: unknown }

export class RouteResourceBoundary extends Component<RouteResourceBoundaryProps, RouteResourceBoundaryState> {
  state: RouteResourceBoundaryState = { error: null }

  static getDerivedStateFromError(error: unknown): Pick<RouteResourceBoundaryState, 'error'> {
    return { error }
  }

  private reloadRouteResources = () => {
    window.location.reload()
  }

  render() {
    if (this.state.error !== null) {
      if (!(this.state.error instanceof RouteResourceLoadError)) throw this.state.error
      return <RouteResourceError onReload={this.reloadRouteResources} />
    }
    return this.props.children
  }
}

function RouteResourceError({ onReload }: { onReload: () => void }) {
  const titleRef = useRef<HTMLHeadingElement>(null)
  useLayoutEffect(() => { titleRef.current?.focus({ preventScroll: true }) }, [])

  return <div className="route-resource-error">
    <Notice tone="error">
      <h1 ref={titleRef} tabIndex={-1}>页面资源加载失败</h1>
      <p>当前页面资源未能加载，请重新加载当前页面以获取资源。</p>
    </Notice>
    <Button onClick={onReload}>重新加载页面资源</Button>
  </div>
}

const UserOverviewPage = createLazyRoute(() => import('./pages/UserOverviewPage').then((module) => ({ default: module.UserOverviewPage })))
const DevicesPage = createLazyRoute(() => import('./pages/DevicesPage').then((module) => ({ default: module.DevicesPage })))
const UserSubscriptionPage = createLazyRoute(() => import('./pages/UserSubscriptionPage').then((module) => ({ default: module.UserSubscriptionPage })))
const UserOrdersPage = createLazyRoute(() => import('./pages/UserOrdersPage').then((module) => ({ default: module.UserOrdersPage })))
const UserUsagePage = createLazyRoute(() => import('./pages/UserUsagePage').then((module) => ({ default: module.UserUsagePage })))
const SecurityPage = createLazyRoute(() => import('./pages/SecurityPage').then((module) => ({ default: module.SecurityPage })))
const ForbiddenPage = createLazyRoute(() => import('./pages/ForbiddenPage').then((module) => ({ default: module.ForbiddenPage })))

const OverviewPage = createLazyRoute(() => import('./pages/OverviewPage').then((module) => ({ default: module.OverviewPage })))
const EdgesPage = createLazyRoute(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgesPage })))
const EdgeDetailPage = createLazyRoute(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgeDetailPage })))
const DaemonsPage = createLazyRoute(() => import('./pages/DaemonsPage').then((module) => ({ default: module.DaemonsPage })))
const ConnectionsPage = createLazyRoute(() => import('./pages/ConnectionsPage').then((module) => ({ default: module.ConnectionsPage })))
const AccountsPage = createLazyRoute(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountsPage })))
const AccountDetailPage = createLazyRoute(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountDetailPage })))
const PlansPage = createLazyRoute(() => import('./pages/PlansPage').then((module) => ({ default: module.PlansPage })))
const SubscriptionsPage = createLazyRoute(() => import('./pages/SubscriptionsPage').then((module) => ({ default: module.SubscriptionsPage })))
const OrdersPage = createLazyRoute(() => import('./pages/OrdersPage').then((module) => ({ default: module.OrdersPage })))
const CertificatesPage = createLazyRoute(() => import('./pages/CertificatesPage').then((module) => ({ default: module.CertificatesPage })))
const UsagePage = createLazyRoute(() => import('./pages/UsagePage').then((module) => ({ default: module.UsagePage })))
const AuditPage = createLazyRoute(() => import('./pages/AuditPage').then((module) => ({ default: module.AuditPage })))
const SystemPage = createLazyRoute(() => import('./pages/SystemPage').then((module) => ({ default: module.SystemPage })))

function LazyRouteGroup() {
  const context = useOutletContext()
  const location = useLocation()
  return <RouteResourceBoundary key={location.pathname}>
    <Suspense fallback={<Skeleton rows={6} />}><Outlet context={context} /></Suspense>
  </RouteResourceBoundary>
}

export default function App() {
  return <Routes>
    <Route path="/" element={<LandingPage />} />
    <Route path="/login" element={<LoginPage />} />
    <Route path="/app" element={<CloudShell />}>
      <Route index element={<Navigate to="overview" replace />} />
      <Route element={<LazyRouteGroup />}>
        <Route path="overview" element={<UserOverviewPage />} />
        <Route path="devices" element={<DevicesPage />} />
        <Route path="subscription" element={<UserSubscriptionPage />} />
        <Route path="orders" element={<UserOrdersPage />} />
        <Route path="usage" element={<UserUsagePage />} />
        <Route path="security" element={<SecurityPage />} />
        <Route path="no-permission" element={<ForbiddenPage />} />
        <Route path="admin" element={<AdminGuard />}>
          <Route index element={<Navigate to="overview" replace />} />
          <Route element={<LazyRouteGroup />}>
            <Route path="overview" element={<OverviewPage />} />
            <Route path="edges" element={<EdgesPage />} />
            <Route path="edges/:edgeId/:tab" element={<EdgeDetailPage />} />
            <Route path="daemons" element={<DaemonsPage />} />
            <Route path="connections" element={<ConnectionsPage />} />
            <Route path="accounts" element={<AccountsPage />} />
            <Route path="accounts/:accountId" element={<AccountDetailPage />} />
            <Route path="plans" element={<PlansPage />} />
            <Route path="subscriptions" element={<SubscriptionsPage />} />
            <Route path="orders" element={<OrdersPage />} />
            <Route path="certificates" element={<CertificatesPage />} />
            <Route path="usage" element={<UsagePage />} />
            <Route path="audit" element={<AuditPage />} />
            <Route path="system" element={<SystemPage />} />
          </Route>
        </Route>
      </Route>
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
