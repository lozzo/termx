import { Component, createContext, lazy, Suspense, useContext, useLayoutEffect, useRef, type ComponentType, type LazyExoticComponent, type ReactNode } from 'react'
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

type RouteLoadAttempt = {
  generation: number
  pages: Map<RouteModuleLoader, LazyExoticComponent<ComponentType>>
}

function createRouteLoadAttempt(generation: number): RouteLoadAttempt {
  return { generation, pages: new Map() }
}

const RouteLoadAttempt = createContext(createRouteLoadAttempt(0))

export function createRetryableLazyRoute(loader: RouteModuleLoader): ComponentType {
  return function RetryableLazyRoute() {
    const attempt = useContext(RouteLoadAttempt)
    let Page = attempt.pages.get(loader)
    if (!Page) {
      Page = lazy(async () => {
        try {
          return await loader()
        } catch (error) {
          throw new RouteResourceLoadError(error)
        }
      })
      attempt.pages.set(loader, Page)
    }
    return <Page />
  }
}

type RouteResourceBoundaryProps = { children: ReactNode }
type RouteResourceBoundaryState = { error: unknown; attempt: RouteLoadAttempt }

export class RouteResourceBoundary extends Component<RouteResourceBoundaryProps, RouteResourceBoundaryState> {
  state: RouteResourceBoundaryState = { error: null, attempt: createRouteLoadAttempt(0) }

  static getDerivedStateFromError(error: unknown): Pick<RouteResourceBoundaryState, 'error'> {
    return { error }
  }

  private retry = () => {
    this.setState(({ attempt }) => ({ error: null, attempt: createRouteLoadAttempt(attempt.generation + 1) }))
  }

  private reloadRouteResources = () => {
    window.location.reload()
  }

  render() {
    if (this.state.error !== null) {
      if (!(this.state.error instanceof RouteResourceLoadError)) throw this.state.error
      const retried = this.state.attempt.generation > 0
      return <RouteResourceError generation={this.state.attempt.generation} reload={retried} onRetry={retried ? this.reloadRouteResources : this.retry} />
    }
    return <RouteLoadAttempt value={this.state.attempt}>{this.props.children}</RouteLoadAttempt>
  }
}

function RouteResourceError({ generation, reload, onRetry }: { generation: number; reload: boolean; onRetry: () => void }) {
  const titleRef = useRef<HTMLHeadingElement>(null)
  useLayoutEffect(() => { titleRef.current?.focus({ preventScroll: true }) }, [generation])

  return <div className="route-resource-error">
    <Notice tone="error">
      <h1 ref={titleRef} tabIndex={-1}>页面资源加载失败</h1>
      <p>{reload ? '页面资源重试失败，请重新加载当前页面以获取资源。' : '当前页面资源未能加载，请检查网络连接后重试。'}</p>
    </Notice>
    <Button onClick={onRetry}>{reload ? '重新加载页面资源' : '重试加载页面'}</Button>
  </div>
}

const UserOverviewPage = createRetryableLazyRoute(() => import('./pages/UserOverviewPage').then((module) => ({ default: module.UserOverviewPage })))
const DevicesPage = createRetryableLazyRoute(() => import('./pages/DevicesPage').then((module) => ({ default: module.DevicesPage })))
const UserSubscriptionPage = createRetryableLazyRoute(() => import('./pages/UserSubscriptionPage').then((module) => ({ default: module.UserSubscriptionPage })))
const UserOrdersPage = createRetryableLazyRoute(() => import('./pages/UserOrdersPage').then((module) => ({ default: module.UserOrdersPage })))
const UserUsagePage = createRetryableLazyRoute(() => import('./pages/UserUsagePage').then((module) => ({ default: module.UserUsagePage })))
const SecurityPage = createRetryableLazyRoute(() => import('./pages/SecurityPage').then((module) => ({ default: module.SecurityPage })))
const ForbiddenPage = createRetryableLazyRoute(() => import('./pages/ForbiddenPage').then((module) => ({ default: module.ForbiddenPage })))

const OverviewPage = createRetryableLazyRoute(() => import('./pages/OverviewPage').then((module) => ({ default: module.OverviewPage })))
const EdgesPage = createRetryableLazyRoute(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgesPage })))
const EdgeDetailPage = createRetryableLazyRoute(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgeDetailPage })))
const DaemonsPage = createRetryableLazyRoute(() => import('./pages/DaemonsPage').then((module) => ({ default: module.DaemonsPage })))
const ConnectionsPage = createRetryableLazyRoute(() => import('./pages/ConnectionsPage').then((module) => ({ default: module.ConnectionsPage })))
const AccountsPage = createRetryableLazyRoute(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountsPage })))
const AccountDetailPage = createRetryableLazyRoute(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountDetailPage })))
const PlansPage = createRetryableLazyRoute(() => import('./pages/PlansPage').then((module) => ({ default: module.PlansPage })))
const SubscriptionsPage = createRetryableLazyRoute(() => import('./pages/SubscriptionsPage').then((module) => ({ default: module.SubscriptionsPage })))
const OrdersPage = createRetryableLazyRoute(() => import('./pages/OrdersPage').then((module) => ({ default: module.OrdersPage })))
const CertificatesPage = createRetryableLazyRoute(() => import('./pages/CertificatesPage').then((module) => ({ default: module.CertificatesPage })))
const UsagePage = createRetryableLazyRoute(() => import('./pages/UsagePage').then((module) => ({ default: module.UsagePage })))
const AuditPage = createRetryableLazyRoute(() => import('./pages/AuditPage').then((module) => ({ default: module.AuditPage })))
const SystemPage = createRetryableLazyRoute(() => import('./pages/SystemPage').then((module) => ({ default: module.SystemPage })))

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
