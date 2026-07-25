import { create } from "@bufbuild/protobuf";
import {
  Activity,
  Cable,
  ChevronDown,
  CreditCard,
  Gauge,
  KeyRound,
  Laptop,
  LogOut,
  Plus,
  QrCode,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  Smartphone,
  UserRound,
  X,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import {
  CreateManagementCommandRequestSchema,
  CreateManagementCommandResponseSchema,
  ListAccountDevicesRequestSchema,
  ListAccountDevicesResponseSchema,
  ListAccountTopologyRequestSchema,
  ListAccountTopologyResponseSchema,
  ListManagementCommandsRequestSchema,
  ListManagementCommandsResponseSchema,
  ManagementCommandKind,
  ManagementCommandTargetSchema,
  GetOperatorWorkspaceResponseSchema,
  OperatorWorkspaceModule,
  PageRequestSchema,
  RecentAuthenticationRequestSchema,
  RecentAuthenticationResponseSchema,
  RevokeCloudDeviceTargetSchema,
  type ListAccountDevicesResponse,
  type ListAccountTopologyResponse,
  type ListManagementCommandsResponse,
} from "@/generated/cloudpb/cloud_management_pb";
import { KickPresenceTargetSchema } from "@/generated/cloudpb/cloud_hub_control_pb";
import {
	ApproveDaemonEnrollmentRequestSchema,
	ApproveDaemonEnrollmentResponseSchema,
	CreateDaemonEnrollmentRequestSchema,
	DaemonEnrollmentAction,
	DaemonEnrollmentProjectionSchema,
	DaemonEnrollmentState,
	InspectDaemonEnrollmentRequestSchema,
	MobileActivationApproveRequestSchema,
  MobileActivationApproveResponseSchema,
  MobileActivationCreateRequestSchema,
  MobileActivationInspectRequestSchema,
  MobileActivationProjectionSchema,
  MobileActivationState,
  type MobileActivationProjection,
	type DaemonEnrollmentProjection,
} from "@/generated/cloudpb/cloud_companion_pb";
import {
  ConfirmTestPaymentRequestSchema,
  ConfirmTestPaymentResponseSchema,
  CreateCheckoutRequestSchema,
  CreateCheckoutResponseSchema,
  BillingCadence,
  GetAccountCommerceResponseSchema,
  GetAccountRelayQuotaRequestSchema,
  GetAccountRelayQuotaResponseSchema,
  LogoutAccountSessionRequestSchema,
  LogoutAccountSessionResponseSchema,
  PaymentEventType,
  SubscriptionStatus,
  SubscriptionTransitionKind,
  TransitionSubscriptionRequestSchema,
  TransitionSubscriptionResponseSchema,
  type GetAccountCommerceResponse,
  type GetAccountRelayQuotaResponse,
} from "@/generated/cloudpb/cloud_product_pb";
import {
  Availability,
  Freshness,
  ManagedDeviceKind,
  ObservedPath,
} from "@/generated/cloudpb/cloud_topology_pb";
import { ProtoHTTPError, protoGet, protoPost } from "@/protoApi";
import { intlLocale } from "@/i18n";

type Tab = "overview" | "devices" | "plans" | "account";
type AccountState = {
  commerce: GetAccountCommerceResponse;
  quota: GetAccountRelayQuotaResponse;
  devices: ListAccountDevicesResponse;
  topology: ListAccountTopologyResponse;
  commands: ListManagementCommandsResponse;
};
const tabs: [Tab, typeof Gauge][] = [
  ["overview", Gauge],
  ["devices", Laptop],
  ["plans", CreditCard],
  ["account", UserRound],
];

type ProtectedAction = {
  label: string;
  execute: () => Promise<unknown>;
};

export default function AccountPage() {
  const { t, i18n } = useTranslation();
  const [tab, setTab] = useState<Tab>("overview");
  const [state, setState] = useState<AccountState>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [password, setPassword] = useState("");
  const [promotionCode, setPromotionCode] = useState("");
  const [protectedAction, setProtectedAction] = useState<ProtectedAction>();
  const [addDeviceOpen, setAddDeviceOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [removedDevicesOpen, setRemovedDevicesOpen] = useState(false);
  const [operatorModules, setOperatorModules] = useState<OperatorWorkspaceModule[]>([]);

  async function load() {
    try {
      try {
        const workspace = await protoGet("/api/v1/operator/workspace", GetOperatorWorkspaceResponseSchema);
        setOperatorModules(workspace.modules);
      } catch (cause) {
        if (cause instanceof ProtoHTTPError && cause.status === 403) setOperatorModules([]);
        else throw cause;
      }
      const page = create(PageRequestSchema, { pageSize: 100 });
      const [commerce, quota, devices, topology, commands] = await Promise.all([
        protoGet("/api/v1/account/commerce", GetAccountCommerceResponseSchema),
        protoPost(
          "/api/v1/management/relay/quota",
          GetAccountRelayQuotaRequestSchema,
          create(GetAccountRelayQuotaRequestSchema),
          GetAccountRelayQuotaResponseSchema,
        ),
        protoPost(
          "/api/v1/management/devices/list",
          ListAccountDevicesRequestSchema,
          create(ListAccountDevicesRequestSchema, {
            includeRevoked: true,
            page,
          }),
          ListAccountDevicesResponseSchema,
        ),
        protoPost(
          "/api/v1/management/topology/list",
          ListAccountTopologyRequestSchema,
          create(ListAccountTopologyRequestSchema, { page }),
          ListAccountTopologyResponseSchema,
        ),
        protoPost(
          "/api/v1/management/commands/list",
          ListManagementCommandsRequestSchema,
          create(ListManagementCommandsRequestSchema, { page }),
          ListManagementCommandsResponseSchema,
        ),
      ]);
      setState({ commerce, quota, devices, topology, commands });
    } catch (cause) {
      if (cause instanceof ProtoHTTPError && cause.status === 401)
        location.href = "/login";
      else
        setError(
          cause instanceof Error ? cause.message : t("account.requestFailed"),
        );
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function run(key: string, action: () => Promise<unknown>) {
    setBusy(key);
    setError("");
    try {
      await action();
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("account.requestFailed"));
    }
    setBusy("");
  }

  async function confirmProtectedAction() {
    if (!protectedAction) return;
    setBusy("reauth");
    setError("");
    try {
      await protoPost(
        "/api/v1/management/reauth",
        RecentAuthenticationRequestSchema,
        create(RecentAuthenticationRequestSchema, { password }),
        RecentAuthenticationResponseSchema,
      );
      const action = protectedAction;
      setPassword("");
      setProtectedAction(undefined);
      setBusy("command");
      await action.execute();
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("account.requestFailed"));
    } finally {
      setBusy("");
    }
  }

  function protect(label: string, execute: () => Promise<unknown>) {
    setPassword("");
    setProtectedAction({ label, execute });
  }

  async function logout() {
    await protoPost(
      "/api/v1/account/logout",
      LogoutAccountSessionRequestSchema,
      create(LogoutAccountSessionRequestSchema),
      LogoutAccountSessionResponseSchema,
    );
    location.href = "/login";
  }

  if (!state)
    return (
      <main className="grid min-h-dvh place-items-center bg-background text-muted-foreground">
        {t("account.loading")}
      </main>
    );
  const { commerce, quota, devices, topology, commands } = state;
  const activeDevices = devices.devices.filter((device) => !device.revoked);
  const removedDevices = devices.devices.filter((device) => device.revoked);
  const onlineDaemons = activeDevices.filter(
    (device) =>
      device.deviceKind === ManagedDeviceKind.DAEMON &&
      device.presence?.availability === Availability.ONLINE,
  ).length;
  return (
    <div className="min-h-dvh bg-background text-foreground md:grid md:grid-cols-[232px_minmax(0,1fr)]">
      <a className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:bg-primary focus:px-4 focus:py-3 focus:text-primary-foreground" href="#account-content">
        {t("account.skip")}
      </a>
      <aside className="border-b border-line bg-panel px-4 py-3 md:min-h-dvh md:border-b-0 md:border-r md:p-5">
        <div className="flex items-center justify-between gap-3 md:block">
        <a className="flex h-12 items-center gap-3" href="/" aria-label={t("common.home")}>
          <b className="grid size-8 place-items-center bg-primary font-mono text-xs text-primary-foreground">
            MV
          </b>
          <span className="font-medium">Muxvia Cloud</span>
        </a>
        <LanguageSwitcher compact />
        </div>
        <nav className="mt-3 grid grid-cols-4 border border-line md:mt-8 md:grid-cols-1 md:border-0" aria-label={t("common.primaryNavigation")}>
          {tabs.map(([id, Icon]) => (
            <button
              key={id}
              className={`flex min-h-12 cursor-pointer items-center justify-center gap-2 border-r border-line px-2 text-xs last:border-r-0 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary md:justify-start md:border-b md:border-r-0 ${tab === id ? "bg-soft font-semibold text-primary" : "text-muted-foreground hover:bg-soft/70 hover:text-foreground"}`}
              onClick={() => setTab(id)}
              aria-current={tab === id ? "page" : undefined}
            >
              <Icon className="size-4" />
              <span className="max-md:sr-only">{t(`account.tabs.${id}`)}</span>
            </button>
          ))}
        </nav>
        {operatorModules.length > 0 && (
          <section className="mt-3 border border-line md:mt-6 md:border-0" aria-label={t("account.admin.navigation")}>
            <p className="border-b border-line px-3 py-2 font-mono text-[10px] font-semibold text-muted-foreground md:px-0">
              {t("account.admin.navigation")}
            </p>
            <nav className="grid grid-cols-3 md:grid-cols-1" aria-label={t("account.admin.navigation")}>
              {operatorTabs.filter(([module]) => operatorModules.includes(module)).map(([module, Icon, label, anchor]) => (
                <a
                  className="flex min-h-12 min-w-0 items-center justify-center gap-2 border-b border-r border-line px-2 text-xs text-muted-foreground hover:bg-soft/70 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary md:justify-start md:border-r-0"
                  href={`/operator#${anchor}`}
                  key={module}
                >
                  <Icon className="size-4 shrink-0" />
                  <span className="truncate">{t(`account.admin.modules.${label}`)}</span>
                </a>
              ))}
            </nav>
          </section>
        )}
        <div className="mt-8 hidden border-t border-line pt-5 text-xs md:block">
          <strong>{commerce.account?.displayName}</strong>
          <p className="truncate text-muted-foreground">
            {commerce.account?.email}
          </p>
          <Button
            className="mt-4 w-full justify-start"
            variant="outline"
            onClick={logout}
          >
            <LogOut />
            {t("account.signOut")}
          </Button>
        </div>
      </aside>
      <main className="min-w-0 p-4 sm:p-6 md:p-10" id="account-content">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b border-line pb-6">
          <div>
            <p className="font-mono text-[10px] text-primary">
              {t("account.cloudControl")}
            </p>
            <h1 className="mt-2 text-3xl font-semibold">{t(`account.tabs.${tab}`)}</h1>
          </div>
          <Button
            variant="outline"
            size="icon"
            title={t("account.actions.refresh")}
            aria-label={t("account.actions.refresh")}
            onClick={() => void load()}
          >
            <RefreshCw />
          </Button>
        </header>
        {error && (
          <p
            className="mt-5 border border-destructive p-3 text-xs text-destructive"
            role="alert"
          >
            {error}
          </p>
        )}
        {tab === "overview" && (
          <div className="mt-6 grid gap-4 lg:grid-cols-3">
            <Metric label={t("account.overview.nodes")} value={t("account.overview.nodesValue", { active: onlineDaemons, total: activeDevices.length })} />
            <Metric label={t("account.overview.plan")} value={commerce.subscription?.planId ?? "-"} />
            <Metric
              label={t("account.overview.relayRemaining")}
              value={bytes(quota.period?.remainingBytes ?? 0n, intlLocale(i18n.language))}
            />
            <Panel title={t("account.overview.system")} className="lg:col-span-2">
              <div className="grid gap-4 p-5 sm:grid-cols-2">
                <StatusLine icon={<ShieldCheck />} label={t("account.overview.system")} value={t("account.overview.operational")} tone="success" />
                <StatusLine icon={<Cable />} label={t("account.overview.route")} value={t("account.overview.routeValue")} />
              </div>
            </Panel>
            <Panel title={t("account.overview.subscription")}>
              <Row label={t("account.billing.statusLabel")} value={subscriptionStatusLabel(commerce.subscription?.status ?? 0, t)} />
              <Row label={t("account.overview.p2pSessions")} value={String(commerce.entitlement?.capability?.managedP2pMaxConcurrency ?? 0)} />
            </Panel>
            <Panel title={t("account.overview.activity")} className="lg:col-span-3">
              {commerce.audit.length === 0 ? <Empty /> : commerce.audit
                .slice(-5)
                .reverse()
                .map((item) => (
                  <Row
                    key={item.auditId}
                    label={when(item.occurredAtUnixMillis, intlLocale(i18n.language))}
                    value={item.action}
                  />
                ))}
            </Panel>
            <p className="lg:col-span-3 m-0 border-l-2 border-primary bg-panel px-4 py-3 text-sm leading-6 text-muted-foreground">{t("account.overview.proof")}</p>
          </div>
        )}
        {tab === "devices" && (
          <div className="mt-6 grid gap-5">
            <section className="flex flex-col gap-4 border border-line bg-panel p-5 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 className="m-0 text-lg font-semibold">{t("account.devices.title")}</h2>
                <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{t("account.devices.copy")}</p>
              </div>
              <Button className="shrink-0" onClick={() => setAddDeviceOpen(true)}><Plus />{t("account.devices.add")}</Button>
            </section>
            <Panel title={t("account.nodes.registered")}>
              {activeDevices.length === 0 ? <Empty /> : activeDevices.map((device) => (
              <div
                className="grid gap-3 border-b border-line p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
                key={device.deviceId}
                data-testid={`account-device-${device.deviceId}`}
              >
                <div className="min-w-0">
                  <strong className="block truncate text-sm">
                    {device.displayName || device.deviceId}
                  </strong>
                  <p className="mt-1 text-xs text-muted-foreground">{t(`account.nodes.kind.${device.deviceKind === ManagedDeviceKind.DAEMON ? "daemon" : "client"}`)} · {deviceStatus(device, t)}</p>
                  <details className="mt-2 text-xs text-muted-foreground">
                    <summary className="cursor-pointer select-none text-primary">{t("account.devices.details")}</summary>
                    <p className="mt-2 break-all font-mono text-[10px]">{device.deviceId}</p>
                  </details>
                </div>
                <Button
                  variant="outline"
                  disabled={busy === "command"}
                  onClick={() => protect(t(device.deviceKind === ManagedDeviceKind.DAEMON ? "account.nodes.revoke" : "account.nodes.removeAccess"), () =>
                    createManagementCommand(
                      ManagementCommandKind.REVOKE_CLOUD_DEVICE,
                      create(ManagementCommandTargetSchema, {
                        target: {
                          case: "cloudDevice",
                          value: create(RevokeCloudDeviceTargetSchema, {
                            deviceId: device.deviceId,
                            expectedAuthEpoch: device.authEpoch,
                          }),
                        },
                      }),
                    ))}
                >
                  <ShieldAlert />
                  {t(device.deviceKind === ManagedDeviceKind.DAEMON ? "account.nodes.revoke" : "account.nodes.removeAccess")}
                </Button>
              </div>
              ))}
            </Panel>
            {removedDevices.length > 0 && (
              <section className="border border-line bg-panel">
                <button
                  className="flex min-h-12 w-full cursor-pointer items-center justify-between px-4 text-left text-sm font-semibold focus-visible:outline-2 focus-visible:outline-primary"
                  onClick={() => setRemovedDevicesOpen((value) => !value)}
                  aria-expanded={removedDevicesOpen}
                  data-testid="removed-devices-toggle"
                >
                  <span className="flex items-center gap-2 text-muted-foreground"><ShieldAlert className="size-4" />{t("account.devices.removed", { count: removedDevices.length })}</span>
                  <ChevronDown className={`size-4 text-muted-foreground transition-transform ${removedDevicesOpen ? "rotate-180" : ""}`} />
                </button>
                {removedDevicesOpen && (
                  <div className="border-t border-line" data-testid="removed-devices-list">
                    <p className="m-0 border-b border-line px-4 py-3 text-xs leading-5 text-muted-foreground">{t("account.devices.removedCopy")}</p>
                    {removedDevices.map((device) => (
                      <div className="border-b border-line px-4 py-3 last:border-b-0" key={device.deviceId} data-testid={`removed-device-${device.deviceId}`}>
                        <strong className="block truncate text-sm font-medium">{device.displayName || device.deviceId}</strong>
                        <p className="mt-1 text-xs text-muted-foreground">{t(`account.nodes.kind.${device.deviceKind === ManagedDeviceKind.DAEMON ? "daemon" : "client"}`)} · {t("account.nodes.revoked")}</p>
                        <p className="mt-2 break-all font-mono text-[10px] text-muted-foreground">{device.deviceId}</p>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            )}
            <button className="flex min-h-12 cursor-pointer items-center justify-between border border-line bg-panel px-4 text-left text-sm font-semibold focus-visible:outline-2 focus-visible:outline-primary" onClick={() => setAdvancedOpen((value) => !value)} aria-expanded={advancedOpen}>
              <span className="flex items-center gap-2"><Activity className="size-4 text-muted-foreground" />{t("account.devices.advanced")}</span>
              <ChevronDown className={`size-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`} />
            </button>
            {advancedOpen && <AdvancedControls topology={topology} commands={commands} busy={busy} protect={protect} />}
          </div>
        )}
        {tab === "plans" && (
          <div className="mt-6 grid gap-5 lg:grid-cols-2">
            <Panel title={t("account.billing.subscription")}>
              <Row label={t("account.billing.current")} value={commerce.subscription?.planId ?? "-"} />
              <Row
                label={t("account.billing.statusLabel")}
                value={subscriptionStatusLabel(commerce.subscription?.status ?? 0, t)}
              />
              <label className="grid gap-2 border-t border-line p-4 text-xs font-medium">
                {t("account.billing.promotionCode")}
                <Input data-testid="checkout-promotion-code" value={promotionCode} onChange={(event) => setPromotionCode(event.target.value)} placeholder={t("account.billing.promotionCodePlaceholder")} />
              </label>
              <div className="flex flex-wrap gap-2 p-4">
                <Button
                  disabled={
                    busy === "billing" ||
                    commerce.subscription?.planId === "pro"
                  }
                  onClick={() =>
                    void run("billing", async () => {
                      const checkout = await protoPost(
                        "/api/v1/checkout",
                        CreateCheckoutRequestSchema,
                        create(CreateCheckoutRequestSchema, {
                          planId: "pro",
                          requestedTransition:
                            SubscriptionTransitionKind.UPGRADE,
                          billingCadence: BillingCadence.MONTHLY,
                          promotionCode: promotionCode.trim(),
                        }),
                        CreateCheckoutResponseSchema,
                      );
                      if (checkout.checkoutUrl) {
                        window.location.assign(checkout.checkoutUrl);
                        return;
                      }
                      if (checkout.order)
                        await protoPost(
                          "/api/v1/checkout/test-payment",
                          ConfirmTestPaymentRequestSchema,
                          create(ConfirmTestPaymentRequestSchema, {
                            orderId: checkout.order.orderId,
                            eventType: PaymentEventType.SUCCEEDED,
                          }),
                          ConfirmTestPaymentResponseSchema,
                        );
                    })
                  }
                >
                  {t("account.billing.activateTestPro")}
                </Button>
                <Button
                  variant="outline"
                  onClick={() =>
                    void run("billing", () =>
                      protoPost(
                        "/api/v1/subscription/transition",
                        TransitionSubscriptionRequestSchema,
                        create(TransitionSubscriptionRequestSchema, {
                          transition:
                            SubscriptionTransitionKind.CANCEL_AT_PERIOD_END,
                        }),
                        TransitionSubscriptionResponseSchema,
                      ),
                    )
                  }
                >
                  {t("account.billing.cancelRenewal")}
                </Button>
                <Button
                  variant="outline"
                  onClick={() =>
                    void run("billing", () =>
                      protoPost(
                        "/api/v1/subscription/transition",
                        TransitionSubscriptionRequestSchema,
                        create(TransitionSubscriptionRequestSchema, {
                          transition: SubscriptionTransitionKind.RESUME,
                        }),
                        TransitionSubscriptionResponseSchema,
                      ),
                    )
                  }
                >
                  {t("account.billing.resumeRenewal")}
                </Button>
              </div>
            </Panel>
            <Panel title={t("account.billing.ordersAndEvents")}>
              {commerce.orders.map((order) => (
                <Row
                  key={order.orderId}
                  label={order.planId}
                  value={`${order.status} / ${order.orderId}`}
                />
              ))}
              {commerce.paymentEvents.map((event) => (
                <Row
                  key={event.event?.providerEventId}
                  label={event.event?.provider ?? t("account.billing.provider")}
                  value={`${event.state} / ${event.event?.eventType}`}
                />
              ))}
            </Panel>
          </div>
        )}
        {tab === "account" && (
          <div className="mt-6 grid gap-5 lg:grid-cols-[minmax(0,1fr)_280px]">
            <Panel title={t("account.profile.title")}>
              <Row label={t("account.profile.displayName")} value={commerce.account?.displayName ?? "-"} />
              <Row label={t("account.profile.email")} value={commerce.account?.email ?? "-"} />
              <details className="border-t border-line p-4 text-xs text-muted-foreground">
                <summary className="cursor-pointer text-primary">{t("account.devices.details")}</summary>
                <p className="mt-3 break-all font-mono text-[10px]">{commerce.account?.accountId}</p>
              </details>
            </Panel>
            <section className="border border-line bg-panel p-5">
              <UserRound className="size-5 text-primary" />
              <strong className="mt-4 block text-sm">{commerce.account?.displayName}</strong>
              <p className="mt-1 break-all text-xs text-muted-foreground">{commerce.account?.email}</p>
              <Button className="mt-5 w-full justify-start" variant="outline" onClick={logout}><LogOut />{t("account.signOut")}</Button>
            </section>
          </div>
        )}
      </main>
      {addDeviceOpen && <AddDeviceWizard onClose={() => setAddDeviceOpen(false)} onChanged={load} />}
      {protectedAction && (
        <ReauthDialog
          actionLabel={protectedAction.label}
          password={password}
          busy={busy === "reauth"}
          onPasswordChange={setPassword}
          onCancel={() => { setProtectedAction(undefined); setPassword(""); }}
          onConfirm={() => void confirmProtectedAction()}
        />
      )}
    </div>
  );

}

function AddDeviceWizard({ onClose, onChanged }: { onClose: () => void; onChanged: () => Promise<void> }) {
  const { t } = useTranslation();
  const [kind, setKind] = useState<AddDeviceKind>();
  return (
    <div className="fixed inset-0 z-50 grid bg-black/45 p-3 sm:place-items-center sm:p-6" role="dialog" aria-modal="true" aria-labelledby="add-device-title">
      <section className="flex max-h-[calc(100dvh-24px)] w-full max-w-3xl flex-col self-end overflow-hidden border border-line-strong bg-background shadow-2xl sm:self-auto">
        <header className="flex min-h-14 items-center justify-between gap-4 border-b border-line bg-panel px-4">
          <div>
            <h2 className="m-0 text-base font-semibold" id="add-device-title">{t("account.devices.addTitle")}</h2>
            {kind && <p className="m-0 mt-0.5 text-xs text-muted-foreground">{t(`account.devices.${kind}Title`)}</p>}
          </div>
          <Button size="icon" variant="ghost" onClick={onClose} aria-label={t("account.devices.close")}><X /></Button>
        </header>
        <div className="min-h-0 overflow-y-auto p-4 sm:p-6">
          {!kind ? (
            <div>
              <p className="m-0 max-w-2xl text-sm leading-6 text-muted-foreground">{t("account.devices.addCopy")}</p>
              <div className="mt-5 grid gap-3 sm:grid-cols-2">
                <button className="group min-h-36 cursor-pointer border border-line bg-panel p-5 text-left hover:border-primary focus-visible:outline-2 focus-visible:outline-primary" onClick={() => setKind("phone")}>
                  <Smartphone className="size-6 text-primary" />
                  <strong className="mt-5 block text-base">{t("account.devices.phoneTitle")}</strong>
                  <span className="mt-2 block text-sm leading-6 text-muted-foreground">{t("account.devices.phoneCopy")}</span>
                </button>
                <button className="group min-h-36 cursor-pointer border border-line bg-panel p-5 text-left hover:border-primary focus-visible:outline-2 focus-visible:outline-primary" onClick={() => setKind("daemon")}>
                  <Laptop className="size-6 text-primary" />
                  <strong className="mt-5 block text-base">{t("account.devices.daemonTitle")}</strong>
                  <span className="mt-2 block text-sm leading-6 text-muted-foreground">{t("account.devices.daemonCopy")}</span>
                </button>
              </div>
            </div>
          ) : (
            <div>
              <Button className="mb-4" variant="ghost" onClick={() => setKind(undefined)}>{t("account.devices.back")}</Button>
              {kind === "phone" ? <MobileActivationPanel onActivated={onChanged} onDone={onClose} /> : <DaemonEnrollmentPanel onEnrolled={onChanged} onDone={onClose} />}
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function ReauthDialog({
  actionLabel,
  password,
  busy,
  onPasswordChange,
  onCancel,
  onConfirm,
}: {
  actionLabel: string;
  password: string;
  busy: boolean;
  onPasswordChange: (value: string) => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-[60] grid place-items-center bg-black/45 p-4" role="dialog" aria-modal="true" aria-labelledby="reauth-title">
      <section className="w-full max-w-md border border-line-strong bg-panel p-5 shadow-2xl">
        <KeyRound className="size-5 text-primary" />
        <h2 className="mt-4 text-lg font-semibold" id="reauth-title">{t("account.controls.confirmTitle")}</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">{t("account.controls.confirmCopy", { action: actionLabel })}</p>
        <label className="mt-5 grid gap-2 text-sm font-medium">
          {t("account.controls.currentPassword")}
          <Input autoFocus data-testid="recent-password" type="password" value={password} onChange={(event) => onPasswordChange(event.target.value)} />
        </label>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={onCancel}>{t("account.nodes.cancel")}</Button>
          <Button data-testid="unlock-controls" disabled={!password || busy} onClick={onConfirm}><ShieldCheck />{busy ? t("account.controls.confirming") : t("account.controls.confirm")}</Button>
        </div>
      </section>
    </div>
  );
}

function AdvancedControls({
  topology,
  commands,
  busy,
  protect,
}: {
  topology: ListAccountTopologyResponse;
  commands: ListManagementCommandsResponse;
  busy: string;
  protect: (label: string, execute: () => Promise<unknown>) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-5">
      <Panel title={t("account.topology.controlRelations")}>
        {topology.presences.length === 0 ? <Empty /> : topology.presences.map((presence) => (
          <div className="grid gap-3 border-b border-line p-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-center" key={presence.presenceSessionId}>
            <div className="min-w-0"><strong className="block truncate text-sm">{presence.daemonDeviceId}</strong><span className="text-xs text-muted-foreground">{Availability[presence.availability]} / {Freshness[presence.freshness]}</span></div>
            <span className="break-all text-xs text-muted-foreground">{t("account.topology.controlHub", { hub: presence.controlOwnerHubId })}</span>
            <Button variant="outline" disabled={busy === "command"} onClick={() => protect(t("account.topology.disconnect"), () => createManagementCommand(ManagementCommandKind.KICK_PRESENCE, create(ManagementCommandTargetSchema, { target: { case: "presence", value: create(KickPresenceTargetSchema, { daemonDeviceId: presence.daemonDeviceId, assignmentEpoch: presence.assignmentEpoch, presenceSessionId: presence.presenceSessionId }) } })))}>{t("account.topology.disconnect")}</Button>
          </div>
        ))}
      </Panel>
      <Panel title={t("account.topology.dataPaths")}>
        {topology.peerSessions.length === 0 ? <Empty /> : topology.peerSessions.map((session) => (
          <div className="grid gap-3 border-b border-line p-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-center" key={`${session.target?.managedSessionId}-${session.target?.sessionIncarnation}`}>
            <div className="min-w-0"><strong className="block break-all text-xs">{t("account.topology.peer", { client: session.clientDeviceId, daemon: session.target?.daemonDeviceId })}</strong><span className="text-xs text-muted-foreground">{t("account.topology.dataPath", { path: ObservedPath[session.observedDataPath] })}</span></div>
            <span className="break-all text-xs text-muted-foreground">{t("account.topology.controlHub", { hub: session.controlOwnerHubId })}</span>
            <Button variant="outline" disabled={!session.target || busy === "command"} onClick={() => session.target && protect(t("account.topology.closeSession"), () => createManagementCommand(ManagementCommandKind.CLOSE_MANAGED_PEER_SESSION, create(ManagementCommandTargetSchema, { target: { case: "peerSession", value: session.target! } })))}>{t("account.topology.closeSession")}</Button>
          </div>
        ))}
      </Panel>
      <Panel title={t("account.commands.title")}>
        {commands.commands.length === 0 ? <Empty /> : commands.commands.map((item) => (
          <div className="grid gap-2 border-b border-line p-4 sm:grid-cols-[minmax(0,1fr)_auto_auto]" key={item.commandId}>
            <div className="min-w-0"><strong className="text-xs">{ManagementCommandKind[item.commandKind]}</strong><p className="truncate font-mono text-[9px] text-muted-foreground">{item.commandId}</p></div>
            <span className="text-xs">{t("account.commands.children", { count: item.children.length })}</span>
            <span className="text-xs text-muted-foreground">{item.executionState}</span>
          </div>
        ))}
      </Panel>
    </div>
  );
}

function createManagementCommand(
  kind: ManagementCommandKind,
  target: ReturnType<typeof create<typeof ManagementCommandTargetSchema>>,
) {
  return protoPost(
    "/api/v1/management/commands",
    CreateManagementCommandRequestSchema,
    create(CreateManagementCommandRequestSchema, { commandKind: kind, target, idempotencyKey: crypto.randomUUID() }),
    CreateManagementCommandResponseSchema,
  );
}

function deviceStatus(device: ListAccountDevicesResponse["devices"][number], t: (key: string) => string) {
  if (device.revoked) return t("account.nodes.revoked");
  if (!device.presence) return t("account.nodes.offlineStatus");
  return device.presence.availability === Availability.ONLINE ? t("account.nodes.onlineStatus") : t("account.nodes.offlineStatus");
}

function subscriptionStatusLabel(status: SubscriptionStatus, t: (key: string) => string) {
  const key = SubscriptionStatus[status]?.toLowerCase() ?? "unspecified";
  return t(`account.billing.subscriptionStatus.${key}`);
}

function StatusLine({ icon, label, value, tone }: { icon: ReactNode; label: string; value: string; tone?: "success" }) {
  return <div className="flex items-center gap-3"><span className={tone === "success" ? "text-success" : "text-primary"}>{icon}</span><div><span className="block text-xs text-muted-foreground">{label}</span><strong className="mt-0.5 block text-sm">{value}</strong></div></div>;
}

function DaemonEnrollmentPanel({ onEnrolled, onDone }: { onEnrolled: () => Promise<void>; onDone: () => void }) {
	const { t } = useTranslation();
	const [enrollment, setEnrollment] = useState<DaemonEnrollmentProjection>();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");
	const canApprove = enrollment?.state === DaemonEnrollmentState.WAITING_FOR_APPROVAL && (
		enrollment.action === DaemonEnrollmentAction.APPROVE ||
		enrollment.action === DaemonEnrollmentAction.ALREADY_ENROLLED ||
		enrollment.action === DaemonEnrollmentAction.CONFIRM_TRANSFER
	);
	const actionKey = enrollment ? DaemonEnrollmentAction[enrollment.action] : "UNSPECIFIED";

	useEffect(() => {
		if (!enrollment || enrollment.state === DaemonEnrollmentState.APPROVED || enrollment.state === DaemonEnrollmentState.COMPLETED || enrollment.state === DaemonEnrollmentState.REJECTED || enrollment.state === DaemonEnrollmentState.EXPIRED) return;
		let stopped = false;
		const inspect = async () => {
			try {
				const next = await protoPost(
					"/api/v1/daemon-enrollments/inspect",
					InspectDaemonEnrollmentRequestSchema,
					create(InspectDaemonEnrollmentRequestSchema, { userCode: enrollment.userCode }),
					DaemonEnrollmentProjectionSchema,
				);
				if (!stopped) setEnrollment(next);
			} catch (cause) {
				if (!stopped) setError(cause instanceof Error ? cause.message : t("account.daemonFlow.inspectError"));
			}
		};
		const timer = window.setInterval(() => void inspect(), 1500);
		return () => { stopped = true; window.clearInterval(timer); };
	}, [enrollment]);

	async function createEnrollment() {
		setBusy(true); setError("");
		try {
			setEnrollment(await protoPost(
				"/api/v1/daemon-enrollments/create",
				CreateDaemonEnrollmentRequestSchema,
				create(CreateDaemonEnrollmentRequestSchema),
				DaemonEnrollmentProjectionSchema,
			));
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : t("account.daemonFlow.createError"));
		} finally { setBusy(false); }
	}

	async function approve() {
		if (!enrollment || !canApprove) return;
		setBusy(true); setError("");
		try {
			await protoPost(
				"/api/v1/daemon-enrollments/approve",
				ApproveDaemonEnrollmentRequestSchema,
				create(ApproveDaemonEnrollmentRequestSchema, { userCode: enrollment.userCode }),
				ApproveDaemonEnrollmentResponseSchema,
			);
			setEnrollment({ ...enrollment, state: DaemonEnrollmentState.APPROVED });
			await onEnrolled();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : t("account.daemonFlow.approveError"));
		} finally { setBusy(false); }
	}

	return (
		<Panel title={t("account.daemonFlow.title")}>
			{!enrollment ? (
				<div className="flex flex-wrap items-center justify-between gap-4 p-5">
					<div className="flex max-w-2xl items-start gap-3">
						<Laptop className="mt-0.5 size-4 shrink-0 text-primary" />
						<p className="m-0 text-sm leading-6 text-muted-foreground">{t("account.daemonFlow.intro")}</p>
					</div>
					<Button disabled={busy} onClick={() => void createEnrollment()}><KeyRound />{busy ? t("account.daemonFlow.creating") : t("account.daemonFlow.create")}</Button>
				</div>
			) : (
				<div className="p-5">
					<span className="text-xs font-medium text-primary">{t(`account.daemonFlow.state.${DaemonEnrollmentState[enrollment.state]}`)}</span>
					<strong className="mt-3 block break-all font-mono text-xl font-normal">{enrollment.userCode}</strong>
					<code className="mt-4 block overflow-x-auto border border-line bg-white p-3 text-xs">muxvia cloud node enroll {enrollment.userCode}</code>
					{enrollment.daemonMetadata ? (
						<div className="mt-5 border-y border-line py-4 text-xs">
							<b className="block font-medium">{enrollment.daemonMetadata.displayName}</b>
							<span className="text-muted-foreground">{enrollment.daemonMetadata.hostname || enrollment.daemonDeviceId} · {enrollment.daemonMetadata.platform} · {enrollment.daemonMetadata.muxviaVersion}</span>
						</div>
					) : <p className="mt-4 text-sm leading-6 text-muted-foreground">{t("account.daemonFlow.waiting")}</p>}
					{enrollment.daemonMetadata && enrollment.action !== DaemonEnrollmentAction.APPROVE && enrollment.action !== DaemonEnrollmentAction.UNSPECIFIED && <p className="mt-4 border border-line p-3 text-sm leading-6 text-muted-foreground">{t(`account.daemonFlow.action.${actionKey}`)}</p>}
					{canApprove && <Button className="mt-5" disabled={busy} onClick={() => void approve()}><ShieldCheck />{busy ? t("account.daemonFlow.approving") : t(enrollment.action === DaemonEnrollmentAction.CONFIRM_TRANSFER ? "account.daemonFlow.confirmTransfer" : enrollment.action === DaemonEnrollmentAction.ALREADY_ENROLLED ? "account.daemonFlow.replaceSession" : "account.daemonFlow.approve")}</Button>}
					{enrollment.state === DaemonEnrollmentState.APPROVED && <p className="mt-5 text-sm text-success">{t("account.daemonFlow.approved")}</p>}
					<Button className="mt-3" variant="ghost" onClick={() => { if (enrollment.state === DaemonEnrollmentState.APPROVED) onDone(); else { setEnrollment(undefined); setError(""); } }}>{enrollment.state === DaemonEnrollmentState.APPROVED ? t("account.nodes.done") : t("account.nodes.cancel")}</Button>
				</div>
			)}
			{error && <p className="m-5 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
		</Panel>
	);
}

function MobileActivationPanel({ onActivated, onDone }: { onActivated: () => Promise<void>; onDone: () => void }) {
  const { t } = useTranslation();
  const [activation, setActivation] = useState<MobileActivationProjection>();
  const [qrDataURL, setQRDataURL] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!activation?.qrPayload) { setQRDataURL(""); return; }
    void QRCode.toDataURL(activation.qrPayload, { width: 256, margin: 2, errorCorrectionLevel: "M", color: { dark: "#111111", light: "#ffffff" } })
      .then(setQRDataURL)
      .catch(() => setError(t("account.mobileFlow.renderError")));
  }, [activation?.qrPayload]);

  useEffect(() => {
    if (!activation || activation.state !== MobileActivationState.WAITING_FOR_DEVICE) return;
    let stopped = false;
    const inspect = async () => {
      try {
        const next = await protoPost(
          "/api/v1/mobile-activations/inspect",
          MobileActivationInspectRequestSchema,
          create(MobileActivationInspectRequestSchema, { userCode: activation.userCode }),
          MobileActivationProjectionSchema,
        );
        if (!stopped) setActivation(next);
      } catch (cause) {
        if (!stopped) setError(cause instanceof Error ? cause.message : t("account.mobileFlow.inspectError"));
      }
    };
    const timer = window.setInterval(() => void inspect(), 1500);
    return () => { stopped = true; window.clearInterval(timer); };
  }, [activation]);

  async function createActivation() {
    setBusy(true); setError("");
    try {
      setActivation(await protoPost(
        "/api/v1/mobile-activations/create",
        MobileActivationCreateRequestSchema,
        create(MobileActivationCreateRequestSchema),
        MobileActivationProjectionSchema,
      ));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("account.mobileFlow.createError"));
    } finally { setBusy(false); }
  }

  async function approve() {
    if (!activation) return;
    setBusy(true); setError("");
    try {
      await protoPost(
        "/api/v1/mobile-activations/approve",
        MobileActivationApproveRequestSchema,
        create(MobileActivationApproveRequestSchema, { userCode: activation.userCode }),
        MobileActivationApproveResponseSchema,
      );
      setActivation({ ...activation, state: MobileActivationState.APPROVED });
      await onActivated();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("account.mobileFlow.approveError"));
    } finally { setBusy(false); }
  }

  return (
    <Panel title={t("account.nodes.mobileTitle")}>
      {!activation ? (
        <div className="flex flex-wrap items-center justify-between gap-4 p-5">
          <div className="flex max-w-2xl items-start gap-3">
            <Smartphone className="mt-0.5 size-4 shrink-0 text-primary" />
            <p className="m-0 text-sm leading-6 text-muted-foreground">{t("account.nodes.mobileCopy")}</p>
          </div>
          <Button disabled={busy} onClick={() => void createActivation()}><QrCode />{busy ? t("account.nodes.creating") : t("account.nodes.createQR")}</Button>
        </div>
      ) : (
        <div className="grid md:grid-cols-[288px_1fr]">
          <div className="grid min-h-72 place-items-center border-b border-line bg-white p-4 md:border-b-0 md:border-r">
            {qrDataURL ? <img className="aspect-square size-full max-h-64 max-w-64 object-contain" width="256" height="256" alt={t("account.nodes.qrAlt")} src={qrDataURL} /> : <span className="text-sm text-muted-foreground">{t("account.mobileFlow.rendering")}</span>}
          </div>
          <div className="flex flex-col justify-center p-6">
            <span className="text-xs font-medium text-primary">{t(`account.mobileFlow.state.${MobileActivationState[activation.state]}`)}</span>
            <strong className="mt-3 font-mono text-2xl font-normal">{activation.userCode}</strong>
            {activation.clientMetadata ? (
              <div className="mt-5 border-y border-line py-4 text-xs"><b className="block font-medium">{activation.clientMetadata.displayName}</b><span className="text-muted-foreground">{activation.clientMetadata.platform} · {activation.clientMetadata.muxviaVersion}</span></div>
            ) : <p className="mt-4 text-sm leading-6 text-muted-foreground">{t("account.nodes.scanCopy")}</p>}
            {activation.state === MobileActivationState.WAITING_FOR_APPROVAL && <Button className="mt-5 self-start" disabled={busy} onClick={() => void approve()}><ShieldCheck />{busy ? t("account.mobileFlow.approving") : t("account.mobileFlow.approve")}</Button>}
            {activation.state === MobileActivationState.APPROVED && <p className="mt-5 text-sm text-success">{t("account.nodes.approvedCopy")}</p>}
            <Button className="mt-3 self-start" variant="ghost" onClick={() => { if (activation.state === MobileActivationState.APPROVED) onDone(); else { setActivation(undefined); setError(""); } }}>{activation.state === MobileActivationState.APPROVED ? t("account.nodes.done") : t("account.nodes.cancel")}</Button>
          </div>
        </div>
      )}
      {error && <p className="m-5 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
    </Panel>
  );
}

function Panel({
  title,
  className = "",
  children,
}: {
  title: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={`border border-line bg-panel ${className}`}>
      <h2 className="border-b border-line px-4 py-3 text-sm font-medium">
        {title}
      </h2>
      {children}
    </section>
  );
}
function Metric({ label, value }: { label: string; value: string }) {
  return (
    <section className="border border-line bg-panel p-5">
      <p className="font-mono text-[9px] text-muted-foreground">
        {label.toUpperCase()}
      </p>
      <strong className="mt-3 block text-2xl font-light">{value}</strong>
    </section>
  );
}
function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid min-h-12 grid-cols-1 items-center gap-1 border-b border-line px-4 py-3 text-xs last:border-0 sm:grid-cols-[100px_minmax(0,1fr)] sm:gap-3 sm:py-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{value}</span>
    </div>
  );
}
function Empty() {
  const { t } = useTranslation();
  return <p className="p-5 text-sm text-muted-foreground">{t("account.noRecords")}</p>;
}
function bytes(value: bigint, locale: string) {
  return value === 0n
    ? "0 B"
    : `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(Number(value) / 1024 / 1024)} MiB`;
}
function when(value: bigint, locale: string) {
  return value ? new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(Number(value))) : "-";
}
