import { Navigate, Route, Routes } from 'react-router'
import { LoginPage } from './pages/LoginPage'
import { LandingPage } from './pages/LandingPage'
import { AdminGuard, CloudShell } from './shell/CloudShell'
import { UserOverviewPage } from './pages/UserOverviewPage'
import { DevicesPage } from './pages/DevicesPage'
import { UserSubscriptionPage } from './pages/UserSubscriptionPage'
import { UserOrdersPage } from './pages/UserOrdersPage'
import { UserUsagePage } from './pages/UserUsagePage'
import { SecurityPage } from './pages/SecurityPage'
import { ForbiddenPage } from './pages/ForbiddenPage'
import { OverviewPage } from './pages/OverviewPage'
import { EdgesPage, EdgeDetailPage } from './pages/EdgesPage'
import { DaemonsPage } from './pages/DaemonsPage'
import { ConnectionsPage } from './pages/ConnectionsPage'
import { AccountsPage, AccountDetailPage } from './pages/AccountsPage'
import { PlansPage } from './pages/PlansPage'
import { SubscriptionsPage } from './pages/SubscriptionsPage'
import { OrdersPage } from './pages/OrdersPage'
import { CertificatesPage } from './pages/CertificatesPage'
import { UsagePage } from './pages/UsagePage'
import { AuditPage } from './pages/AuditPage'
import { SystemPage } from './pages/SystemPage'

export default function App() {
  return <Routes>
    <Route path="/" element={<LandingPage />} />
    <Route path="/login" element={<LoginPage />} />
    <Route path="/app" element={<CloudShell />}>
      <Route index element={<Navigate to="overview" replace />} />
      <Route path="overview" element={<UserOverviewPage />} />
      <Route path="devices" element={<DevicesPage />} />
      <Route path="subscription" element={<UserSubscriptionPage />} />
      <Route path="orders" element={<UserOrdersPage />} />
      <Route path="usage" element={<UserUsagePage />} />
      <Route path="security" element={<SecurityPage />} />
      <Route path="no-permission" element={<ForbiddenPage />} />
      <Route path="admin" element={<AdminGuard />}>
        <Route index element={<Navigate to="overview" replace />} />
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
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
