import { lazy, Suspense } from 'react'
import { Navigate, Outlet, Route, Routes, useOutletContext } from 'react-router'
import { LoginPage } from './pages/LoginPage'
import { LandingPage } from './pages/LandingPage'
import { AdminGuard, CloudShell } from './shell/CloudShell'
import { Skeleton } from './ui'

const UserOverviewPage = lazy(() => import('./pages/UserOverviewPage').then((module) => ({ default: module.UserOverviewPage })))
const DevicesPage = lazy(() => import('./pages/DevicesPage').then((module) => ({ default: module.DevicesPage })))
const UserSubscriptionPage = lazy(() => import('./pages/UserSubscriptionPage').then((module) => ({ default: module.UserSubscriptionPage })))
const UserOrdersPage = lazy(() => import('./pages/UserOrdersPage').then((module) => ({ default: module.UserOrdersPage })))
const UserUsagePage = lazy(() => import('./pages/UserUsagePage').then((module) => ({ default: module.UserUsagePage })))
const SecurityPage = lazy(() => import('./pages/SecurityPage').then((module) => ({ default: module.SecurityPage })))
const ForbiddenPage = lazy(() => import('./pages/ForbiddenPage').then((module) => ({ default: module.ForbiddenPage })))

const OverviewPage = lazy(() => import('./pages/OverviewPage').then((module) => ({ default: module.OverviewPage })))
const EdgesPage = lazy(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgesPage })))
const EdgeDetailPage = lazy(() => import('./pages/EdgesPage').then((module) => ({ default: module.EdgeDetailPage })))
const DaemonsPage = lazy(() => import('./pages/DaemonsPage').then((module) => ({ default: module.DaemonsPage })))
const ConnectionsPage = lazy(() => import('./pages/ConnectionsPage').then((module) => ({ default: module.ConnectionsPage })))
const AccountsPage = lazy(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountsPage })))
const AccountDetailPage = lazy(() => import('./pages/AccountsPage').then((module) => ({ default: module.AccountDetailPage })))
const PlansPage = lazy(() => import('./pages/PlansPage').then((module) => ({ default: module.PlansPage })))
const SubscriptionsPage = lazy(() => import('./pages/SubscriptionsPage').then((module) => ({ default: module.SubscriptionsPage })))
const OrdersPage = lazy(() => import('./pages/OrdersPage').then((module) => ({ default: module.OrdersPage })))
const CertificatesPage = lazy(() => import('./pages/CertificatesPage').then((module) => ({ default: module.CertificatesPage })))
const UsagePage = lazy(() => import('./pages/UsagePage').then((module) => ({ default: module.UsagePage })))
const AuditPage = lazy(() => import('./pages/AuditPage').then((module) => ({ default: module.AuditPage })))
const SystemPage = lazy(() => import('./pages/SystemPage').then((module) => ({ default: module.SystemPage })))

function LazyRouteGroup() {
  const context = useOutletContext()
  return <Suspense fallback={<Skeleton rows={6} />}><Outlet context={context} /></Suspense>
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
