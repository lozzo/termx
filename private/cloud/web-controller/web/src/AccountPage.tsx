import { create } from "@bufbuild/protobuf";
import {
  Activity,
  Cable,
  CreditCard,
  Gauge,
  KeyRound,
  Laptop,
  LogOut,
  QrCode,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  Smartphone,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import QRCode from "qrcode";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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

type Tab = "overview" | "devices" | "topology" | "commands" | "billing";
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
  ["topology", Cable],
  ["commands", Activity],
  ["billing", CreditCard],
];

export default function AccountPage() {
  const [tab, setTab] = useState<Tab>("overview");
  const [state, setState] = useState<AccountState>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [password, setPassword] = useState("");
  const [controlsUnlocked, setControlsUnlocked] = useState(false);

  async function load() {
    try {
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
          cause instanceof Error ? cause.message : "Could not load account",
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
      setError(cause instanceof Error ? cause.message : "Request failed");
    }
    setBusy("");
  }

  async function unlock() {
    await run("reauth", async () => {
      await protoPost(
        "/api/v1/management/reauth",
        RecentAuthenticationRequestSchema,
        create(RecentAuthenticationRequestSchema, { password }),
        RecentAuthenticationResponseSchema,
      );
      setPassword("");
      setControlsUnlocked(true);
    });
  }

  async function command(
    kind: ManagementCommandKind,
    target: ReturnType<typeof create<typeof ManagementCommandTargetSchema>>,
  ) {
    await run("command", () =>
      protoPost(
        "/api/v1/management/commands",
        CreateManagementCommandRequestSchema,
        create(CreateManagementCommandRequestSchema, {
          commandKind: kind,
          target,
          idempotencyKey: crypto.randomUUID(),
        }),
        CreateManagementCommandResponseSchema,
      ),
    );
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
        Loading account...
      </main>
    );
  const { commerce, quota, devices, topology, commands } = state;
  return (
    <div className="min-h-dvh bg-background text-foreground md:grid md:grid-cols-[220px_minmax(0,1fr)]">
      <aside className="border-r border-line bg-panel p-5 md:min-h-dvh">
        <a className="flex h-12 items-center gap-3" href="/">
          <b className="grid size-8 place-items-center bg-primary font-mono text-xs text-primary-foreground">
            MV
          </b>
          <span className="font-medium">Muxvia Cloud</span>
        </a>
        <nav className="mt-8 grid grid-cols-5 border border-line md:grid-cols-1 md:border-0">
          {tabs.map(([id, Icon]) => (
            <button
              key={id}
              className={`flex min-h-11 items-center justify-center gap-2 border-b border-line px-2 text-xs capitalize md:justify-start ${tab === id ? "bg-soft text-primary" : "text-muted-foreground"}`}
              onClick={() => setTab(id)}
            >
              <Icon className="size-4" />
              <span>{id}</span>
            </button>
          ))}
        </nav>
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
            Sign out
          </Button>
        </div>
      </aside>
      <main className="min-w-0 p-5 md:p-10">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b border-line pb-6">
          <div>
            <p className="font-mono text-[10px] text-primary">
              ACCOUNT CONTROL PLANE
            </p>
            <h1 className="mt-2 text-3xl font-light capitalize">{tab}</h1>
          </div>
          <Button
            variant="outline"
            size="icon"
            title="Refresh"
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
        <section className="mt-6 border border-line bg-panel p-4">
          <div className="flex flex-wrap items-end gap-3">
            <label className="grid min-w-56 flex-1 gap-2 font-mono text-[9px] text-muted-foreground">
              CURRENT PASSWORD
              <Input
                data-testid="recent-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
            <Button
              data-testid="unlock-controls"
              disabled={!password || busy === "reauth"}
              onClick={() => void unlock()}
            >
              <KeyRound />
              {controlsUnlocked ? "Refresh authorization" : "Unlock controls"}
            </Button>
            <span
              className={`text-xs ${controlsUnlocked ? "text-success" : "text-muted-foreground"}`}
            >
              {controlsUnlocked
                ? "Destructive controls unlocked for 5 minutes"
                : "Required for revoke and disconnect"}
            </span>
          </div>
        </section>
        {tab === "overview" && (
          <div className="mt-6 grid gap-4 lg:grid-cols-3">
            <Metric label="Plan" value={commerce.subscription?.planId ?? "-"} />
            <Metric
              label="Subscription"
              value={SubscriptionStatus[commerce.subscription?.status ?? 0]}
            />
            <Metric
              label="Relay remaining"
              value={bytes(quota.period?.remainingBytes ?? 0n)}
            />
            <Panel title="Account">
              <Row
                label="Account ID"
                value={commerce.account?.accountId ?? "-"}
              />
              <Row
                label="Auth revision"
                value={String(commerce.account?.authRevision ?? 0n)}
              />
            </Panel>
            <Panel title="Capability">
              <Row
                label="P2P sessions"
                value={String(
                  commerce.entitlement?.capability?.managedP2pMaxConcurrency ??
                    0,
                )}
              />
              <Row
                label="Relay period"
                value={bytes(
                  commerce.entitlement?.capability?.relay?.maxBytesPerPeriod ??
                    0n,
                )}
              />
            </Panel>
            <Panel title="Recent audit">
              {commerce.audit
                .slice(-5)
                .reverse()
                .map((item) => (
                  <Row
                    key={item.auditId}
                    label={when(item.occurredAtUnixMillis)}
                    value={item.action}
                  />
                ))}
            </Panel>
          </div>
        )}
        {tab === "devices" && (
          <div className="mt-6 grid gap-5">
			<DaemonEnrollmentPanel onEnrolled={load} />
            <MobileActivationPanel onActivated={load} />
            <Panel title="Registered devices">
              {devices.devices.map((device) => (
              <div
                className="grid gap-3 border-b border-line p-4 lg:grid-cols-[1fr_160px_160px_auto] lg:items-center"
                key={device.deviceId}
              >
                <div>
                  <strong className="text-sm">
                    {device.displayName || device.deviceId}
                  </strong>
                  <p className="font-mono text-[10px] text-muted-foreground">
                    {device.deviceId}
                  </p>
                </div>
                <span className="text-xs">
                  {ManagedDeviceKind[device.deviceKind]}
                </span>
                <span className="text-xs">
                  {device.presence
                    ? `${Availability[device.presence.availability]} / ${Freshness[device.presence.freshness]}`
                    : "UNKNOWN / STALE"}
                </span>
                <Button
                  variant="outline"
                  disabled={
                    !controlsUnlocked || device.revoked || busy === "command"
                  }
                  onClick={() =>
                    void command(
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
                    )
                  }
                >
                  <ShieldAlert />
                  Revoke
                </Button>
              </div>
              ))}
            </Panel>
          </div>
        )}
        {tab === "topology" && (
          <div className="mt-6 grid gap-5">
            <Panel title="Signaling control relations">
              {topology.presences.map((presence) => (
                <div
                  className="grid gap-3 border-b border-line p-4 lg:grid-cols-[1fr_1fr_160px_auto] lg:items-center"
                  key={presence.presenceSessionId}
                >
                  <span className="font-mono text-xs">
                    {presence.daemonDeviceId}
                  </span>
                  <span className="text-xs">
                    Control Hub: {presence.controlOwnerHubId}
                  </span>
                  <span className="text-xs">
                    {Availability[presence.availability]} /{" "}
                    {Freshness[presence.freshness]}
                  </span>
                  <Button
                    variant="outline"
                    disabled={!controlsUnlocked || busy === "command"}
                    onClick={() =>
                      void command(
                        ManagementCommandKind.KICK_PRESENCE,
                        create(ManagementCommandTargetSchema, {
                          target: {
                            case: "presence",
                            value: create(KickPresenceTargetSchema, {
                              daemonDeviceId: presence.daemonDeviceId,
                              assignmentEpoch: presence.assignmentEpoch,
                              presenceSessionId: presence.presenceSessionId,
                            }),
                          },
                        }),
                      )
                    }
                  >
                    Disconnect
                  </Button>
                </div>
              ))}
            </Panel>
            <Panel title="Observed data paths">
              {topology.peerSessions.map((session) => (
                <div
                  className="grid gap-3 border-b border-line p-4 lg:grid-cols-[1fr_1fr_160px_auto] lg:items-center"
                  key={`${session.target?.managedSessionId}-${session.target?.sessionIncarnation}`}
                >
                  <span className="font-mono text-xs">
                    {session.clientDeviceId} to {session.target?.daemonDeviceId}
                  </span>
                  <span className="text-xs">
                    Data path: {ObservedPath[session.observedDataPath]}
                  </span>
                  <span className="text-xs">
                    Control Hub: {session.controlOwnerHubId}
                  </span>
                  <Button
                    variant="outline"
                    disabled={
                      !controlsUnlocked || !session.target || busy === "command"
                    }
                    onClick={() =>
                      session.target &&
                      void command(
                        ManagementCommandKind.CLOSE_MANAGED_PEER_SESSION,
                        create(ManagementCommandTargetSchema, {
                          target: {
                            case: "peerSession",
                            value: session.target,
                          },
                        }),
                      )
                    }
                  >
                    Close session
                  </Button>
                </div>
              ))}
            </Panel>
          </div>
        )}
        {tab === "commands" && (
          <Panel title="Command outbox" className="mt-6">
            {commands.commands.length === 0 ? (
              <Empty />
            ) : (
              commands.commands.map((item) => (
                <div
                  className="grid gap-2 border-b border-line p-4 lg:grid-cols-[1fr_160px_160px]"
                  key={item.commandId}
                >
                  <div>
                    <strong className="text-xs">
                      {ManagementCommandKind[item.commandKind]}
                    </strong>
                    <p className="font-mono text-[9px] text-muted-foreground">
                      {item.commandId}
                    </p>
                  </div>
                  <span className="text-xs">
                    {item.children.length} child operations
                  </span>
                  <span className="text-xs">{item.executionState}</span>
                </div>
              ))
            )}
          </Panel>
        )}
        {tab === "billing" && (
          <div className="mt-6 grid gap-5 lg:grid-cols-2">
            <Panel title="Subscription">
              <Row label="Plan" value={commerce.subscription?.planId ?? "-"} />
              <Row
                label="Status"
                value={SubscriptionStatus[commerce.subscription?.status ?? 0]}
              />
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
                        }),
                        CreateCheckoutResponseSchema,
                      );
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
                  Activate Pro with test provider
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
                  Cancel renewal
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
                  Resume renewal
                </Button>
              </div>
            </Panel>
            <Panel title="Orders and provider events">
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
                  label={event.event?.provider ?? "provider"}
                  value={`${event.state} / ${event.event?.eventType}`}
                />
              ))}
            </Panel>
          </div>
        )}
      </main>
    </div>
  );
}

function DaemonEnrollmentPanel({ onEnrolled }: { onEnrolled: () => Promise<void> }) {
	const [enrollment, setEnrollment] = useState<DaemonEnrollmentProjection>();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (!enrollment || enrollment.state === DaemonEnrollmentState.APPROVED) return;
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
				if (!stopped) setError(cause instanceof Error ? cause.message : "Enrollment inspection failed");
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
			setError(cause instanceof Error ? cause.message : "Could not create daemon login code");
		} finally { setBusy(false); }
	}

	async function approve() {
		if (!enrollment) return;
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
			setError(cause instanceof Error ? cause.message : "Could not approve daemon");
		} finally { setBusy(false); }
	}

	return (
		<Panel title="Enroll a daemon server">
			{!enrollment ? (
				<div className="flex flex-wrap items-center justify-between gap-4 p-5">
					<div className="flex max-w-2xl items-start gap-3">
						<Laptop className="mt-0.5 size-4 shrink-0 text-primary" />
						<p className="m-0 text-xs leading-6 text-muted-foreground">Create a ten-minute login code, run it on the daemon host, then review and approve the submitted device identity here.</p>
					</div>
					<Button disabled={busy} onClick={() => void createEnrollment()}><KeyRound />{busy ? "Creating..." : "Create login code"}</Button>
				</div>
			) : (
				<div className="p-5">
					<span className="font-mono text-[10px] text-primary">{DaemonEnrollmentState[enrollment.state]}</span>
					<strong className="mt-3 block break-all font-mono text-xl font-normal">{enrollment.userCode}</strong>
					<code className="mt-4 block overflow-x-auto border border-line bg-white p-3 text-xs">muxvia cloud node enroll {enrollment.userCode}</code>
					{enrollment.daemonMetadata ? (
						<div className="mt-5 border-y border-line py-4 text-xs">
							<b className="block font-medium">{enrollment.daemonMetadata.displayName}</b>
							<span className="text-muted-foreground">{enrollment.daemonMetadata.hostname || enrollment.daemonDeviceId} · {enrollment.daemonMetadata.platform} · {enrollment.daemonMetadata.muxviaVersion}</span>
						</div>
					) : <p className="mt-4 text-xs leading-6 text-muted-foreground">Waiting for the daemon to submit its public identity.</p>}
					{enrollment.state === DaemonEnrollmentState.WAITING_FOR_APPROVAL && <Button className="mt-5" disabled={busy} onClick={() => void approve()}><ShieldCheck />{busy ? "Approving..." : "Approve daemon"}</Button>}
					{enrollment.state === DaemonEnrollmentState.APPROVED && <p className="mt-5 text-xs text-success">Approved. The daemon is completing DeviceIdentity proof and this code cannot enroll another device.</p>}
					<Button className="mt-3" variant="ghost" onClick={() => { setEnrollment(undefined); setError(""); }}>{enrollment.state === DaemonEnrollmentState.APPROVED ? "Done" : "Cancel"}</Button>
				</div>
			)}
			{error && <p className="m-5 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
		</Panel>
	);
}

function MobileActivationPanel({ onActivated }: { onActivated: () => Promise<void> }) {
  const [activation, setActivation] = useState<MobileActivationProjection>();
  const [qrDataURL, setQRDataURL] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!activation?.qrPayload) { setQRDataURL(""); return; }
    void QRCode.toDataURL(activation.qrPayload, { width: 256, margin: 2, errorCorrectionLevel: "M", color: { dark: "#111111", light: "#ffffff" } })
      .then(setQRDataURL)
      .catch(() => setError("Could not render activation QR code"));
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
        if (!stopped) setError(cause instanceof Error ? cause.message : "Activation inspection failed");
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
      setError(cause instanceof Error ? cause.message : "Could not create activation");
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
      setError(cause instanceof Error ? cause.message : "Could not approve phone");
    } finally { setBusy(false); }
  }

  return (
    <Panel title="Activate the mobile app">
      {!activation ? (
        <div className="flex flex-wrap items-center justify-between gap-4 p-5">
          <div className="flex max-w-2xl items-start gap-3">
            <Smartphone className="mt-0.5 size-4 shrink-0 text-primary" />
            <p className="m-0 text-xs leading-6 text-muted-foreground">Create a short-lived QR code, scan it in the Muxvia App, then review and approve the phone here.</p>
          </div>
          <Button disabled={busy} onClick={() => void createActivation()}><QrCode />{busy ? "Creating..." : "Create QR code"}</Button>
        </div>
      ) : (
        <div className="grid md:grid-cols-[288px_1fr]">
          <div className="grid min-h-72 place-items-center border-b border-line bg-white p-4 md:border-b-0 md:border-r">
            {qrDataURL ? <img className="aspect-square size-64 object-contain" width="256" height="256" alt="One-time QR code for activating the Muxvia mobile app" src={qrDataURL} /> : <span className="text-xs text-muted-foreground">Rendering QR code...</span>}
          </div>
          <div className="flex flex-col justify-center p-6">
            <span className="font-mono text-[10px] text-primary">{MobileActivationState[activation.state]}</span>
            <strong className="mt-3 font-mono text-2xl font-normal">{activation.userCode}</strong>
            {activation.clientMetadata ? (
              <div className="mt-5 border-y border-line py-4 text-xs"><b className="block font-medium">{activation.clientMetadata.displayName}</b><span className="text-muted-foreground">{activation.clientMetadata.platform} · {activation.clientMetadata.muxviaVersion}</span></div>
            ) : <p className="mt-4 text-xs leading-6 text-muted-foreground">In the Muxvia App, open Settings and choose Scan QR from web.</p>}
            {activation.state === MobileActivationState.WAITING_FOR_APPROVAL && <Button className="mt-5 self-start" disabled={busy} onClick={() => void approve()}><ShieldCheck />{busy ? "Approving..." : "Approve phone"}</Button>}
            {activation.state === MobileActivationState.APPROVED && <p className="mt-5 text-xs text-success">Approved. The App is finishing activation and this code cannot be reused.</p>}
            <Button className="mt-3 self-start" variant="ghost" onClick={() => { setActivation(undefined); setError(""); }}>{activation.state === MobileActivationState.APPROVED ? "Done" : "Cancel"}</Button>
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
    <div className="grid min-h-12 grid-cols-[130px_1fr] items-center gap-3 border-b border-line px-4 text-xs last:border-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-all">{value}</span>
    </div>
  );
}
function Empty() {
  return <p className="p-5 text-sm text-muted-foreground">No records.</p>;
}
function bytes(value: bigint) {
  return value === 0n
    ? "0 B"
    : `${(Number(value) / 1024 / 1024).toFixed(1)} MiB`;
}
function when(value: bigint) {
  return value ? new Date(Number(value)).toLocaleString() : "-";
}
