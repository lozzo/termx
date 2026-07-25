import { create, fromJsonString, toJsonString } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  Building2,
  History,
  LogOut,
  PackageOpen,
  ReceiptText,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  TicketPercent,
  UserRoundCog,
} from "lucide-react";
import { FormEvent, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  GetOperatorAccountRequestSchema,
  GetOperatorAccountResponseSchema,
  ListHubFleetRequestSchema,
  ListHubFleetResponseSchema,
  ListOperatorAccountsRequestSchema,
  ListOperatorAccountsResponseSchema,
  ListOperatorOrdersRequestSchema,
  ListOperatorOrdersResponseSchema,
  ListOperatorSubscriptionsRequestSchema,
  ListOperatorSubscriptionsResponseSchema,
  CreateSubscriptionAdjustmentRequestSchema,
  CreateSubscriptionAdjustmentResponseSchema,
  ApplyOperatorPaymentEventRequestSchema,
  ApplyOperatorPaymentEventResponseSchema,
  CreatePromotionRequestSchema,
  CreatePromotionResponseSchema,
  ListPromotionsRequestSchema,
  ListPromotionsResponseSchema,
  ListPlanCatalogReleasesRequestSchema,
  ListPlanCatalogReleasesResponseSchema,
  ListEntitlementOverridesRequestSchema,
  ListEntitlementOverridesResponseSchema,
  PublishPlanCatalogRequestSchema,
  PublishPlanCatalogResponseSchema,
  PutEntitlementOverrideRequestSchema,
  PutEntitlementOverrideResponseSchema,
  ManagementActorKind,
  OperatorLoginRequestSchema,
  OperatorLoginResponseSchema,
  OperatorLogoutRequestSchema,
  OperatorLogoutResponseSchema,
  OperatorTransitionSubscriptionRequestSchema,
  OperatorTransitionSubscriptionResponseSchema,
  PageRequestSchema,
  CreateManagementCommandRequestSchema,
  CreateManagementCommandResponseSchema,
  ManagementCommandKind,
  ManagementCommandTargetSchema,
  AssignmentMigrationTargetSchema,
  RevokeCloudDeviceTargetSchema,
  type GetOperatorAccountResponse,
  type ListHubFleetResponse,
  type ListOperatorAccountsResponse,
  type ListOperatorOrdersResponse,
  type ListOperatorSubscriptionsResponse,
  type ListPromotionsResponse,
  type ListPlanCatalogReleasesResponse,
  type ListEntitlementOverridesResponse,
} from "@/generated/cloudpb/cloud_management_pb";
import {
  EntitlementOverrideProjectionSchema,
  PromotionProjectionSchema,
  OrderStatus,
  PaymentEventType,
  PromotionDiscountKind,
  PromotionState,
  PlanCapabilitySchema,
  PlanCatalogContractSchema,
  SubscriptionStatus,
  SubscriptionAdjustmentKind,
  SubscriptionTransitionKind,
  type PlanCatalogContract,
} from "@/generated/cloudpb/cloud_product_pb";
import {
  Freshness,
  ManagedDeviceKind,
} from "@/generated/cloudpb/cloud_topology_pb";
import { ProtoHTTPError, protoPost } from "@/protoApi";

export default function OperatorPage() {
  const [token, setToken] = useState("");
  const [authenticated, setAuthenticated] = useState(false);
  const [accounts, setAccounts] = useState<ListOperatorAccountsResponse>();
  const [orders, setOrders] = useState<ListOperatorOrdersResponse>();
  const [subscriptions, setSubscriptions] = useState<ListOperatorSubscriptionsResponse>();
  const [promotions, setPromotions] = useState<ListPromotionsResponse>();
  const [fleet, setFleet] = useState<ListHubFleetResponse>();
  const [detail, setDetail] = useState<GetOperatorAccountResponse>();
  const [catalogHistory, setCatalogHistory] =
    useState<ListPlanCatalogReleasesResponse>();
  const [overrides, setOverrides] =
    useState<ListEntitlementOverridesResponse>();
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [catalogDraft, setCatalogDraft] = useState("");
  const [catalogReason, setCatalogReason] = useState("");
  const [overridePath, setOverridePath] = useState("cloud_device_limit");
  const [overrideValue, setOverrideValue] = useState("");
  const [overrideReason, setOverrideReason] = useState("");
  const [overrideUntil, setOverrideUntil] = useState("");
  const [adjustmentDays, setAdjustmentDays] = useState("14");
  const [adjustmentPlan, setAdjustmentPlan] = useState("pro");
  const [adjustmentKind, setAdjustmentKind] = useState<SubscriptionAdjustmentKind>(SubscriptionAdjustmentKind.GRANT);
  const [adjustmentReason, setAdjustmentReason] = useState("");
  const [promotionCode, setPromotionCode] = useState("");
  const [promotionPercent, setPromotionPercent] = useState("10");
  const [promotionKind, setPromotionKind] = useState<PromotionDiscountKind>(PromotionDiscountKind.PERCENT);
  const [promotionPlan, setPromotionPlan] = useState("pro");
  const [promotionLimit, setPromotionLimit] = useState("100");
  const [promotionUntil, setPromotionUntil] = useState("");
  const [promotionCreemCode, setPromotionCreemCode] = useState("");
  const [promotionReason, setPromotionReason] = useState("");
  const [paymentReasons, setPaymentReasons] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const catalogEditorRef = useRef<HTMLTextAreaElement>(null);

  function showCatalog(catalog: PlanCatalogContract) {
    setCatalogDraft(toJsonString(PlanCatalogContractSchema, catalog, { prettySpaces: 2 }));
    requestAnimationFrame(() => {
      catalogEditorRef.current?.setSelectionRange(0, 0);
      catalogEditorRef.current?.scrollTo({ top: 0 });
    });
  }

  async function load(search = query) {
    try {
      const page = create(PageRequestSchema, { pageSize: 100 });
      const [nextAccounts, nextFleet, nextCatalogHistory, nextOrders, nextSubscriptions, nextPromotions] = await Promise.all([
        protoPost(
          "/api/v1/operator/accounts/list",
          ListOperatorAccountsRequestSchema,
          create(ListOperatorAccountsRequestSchema, { query: search, page }),
          ListOperatorAccountsResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/fleet/list",
          ListHubFleetRequestSchema,
          create(ListHubFleetRequestSchema, { page }),
          ListHubFleetResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/catalog/list",
          ListPlanCatalogReleasesRequestSchema,
          create(ListPlanCatalogReleasesRequestSchema, { page }),
          ListPlanCatalogReleasesResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/orders/list",
          ListOperatorOrdersRequestSchema,
          create(ListOperatorOrdersRequestSchema, { page }),
          ListOperatorOrdersResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/subscriptions/list",
          ListOperatorSubscriptionsRequestSchema,
          create(ListOperatorSubscriptionsRequestSchema, { page }),
          ListOperatorSubscriptionsResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/promotions/list",
          ListPromotionsRequestSchema,
          create(ListPromotionsRequestSchema, { includeDisabled: true, page }),
          ListPromotionsResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
      ]);
      setAccounts(nextAccounts);
      setFleet(nextFleet);
      setCatalogHistory(nextCatalogHistory);
      setOrders(nextOrders);
      setSubscriptions(nextSubscriptions);
      setPromotions(nextPromotions);
      const active = nextCatalogHistory.releases.find((item) => item.active);
      if (active?.catalog && !catalogDraft)
        setCatalogDraft(
          toJsonString(PlanCatalogContractSchema, active.catalog, {
            prettySpaces: 2,
          }),
        );
      setAuthenticated(true);
      setError("");
    } catch (cause) {
      if (!(cause instanceof ProtoHTTPError && cause.status === 401))
        setError(
          cause instanceof Error ? cause.message : "Operator request failed",
        );
    }
  }

  useEffect(() => {
    void load("");
  }, []);

  async function login(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/login",
        OperatorLoginRequestSchema,
        create(OperatorLoginRequestSchema, { accessToken: decodeToken(token) }),
        OperatorLoginResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setToken("");
      await load("");
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Operator login failed",
      );
    }
  }

  async function select(accountId: string) {
    try {
      const page = create(PageRequestSchema, { pageSize: 100 });
      const [nextDetail, nextOverrides] = await Promise.all([
        protoPost(
          "/api/v1/operator/accounts/get",
          GetOperatorAccountRequestSchema,
          create(GetOperatorAccountRequestSchema, { accountId }),
          GetOperatorAccountResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/entitlement-overrides/list",
          ListEntitlementOverridesRequestSchema,
          create(ListEntitlementOverridesRequestSchema, {
            accountId,
            includeRevoked: true,
            page,
          }),
          ListEntitlementOverridesResponseSchema,
          "muxvia_cloud_operator_csrf",
        ),
      ]);
      setDetail(nextDetail);
      setOverrides(nextOverrides);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Account detail failed",
      );
    }
  }

  async function publishCatalog(event: FormEvent) {
    event.preventDefault();
    if (!catalogReason.trim()) return;
    setBusy(true);
    setError("");
    try {
      const catalog = fromJsonString(PlanCatalogContractSchema, catalogDraft, {
        ignoreUnknownFields: false,
      });
      await protoPost(
        "/api/v1/operator/catalog/publish",
        PublishPlanCatalogRequestSchema,
        create(PublishPlanCatalogRequestSchema, {
          catalog,
          reason: catalogReason.trim(),
          requestId: crypto.randomUUID(),
        }),
        PublishPlanCatalogResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setCatalogReason("");
      await load();
      requestAnimationFrame(() => {
        catalogEditorRef.current?.setSelectionRange(0, 0);
        catalogEditorRef.current?.scrollTo({ top: 0 });
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Catalog publish failed");
    } finally {
      setBusy(false);
    }
  }

  async function putOverride(event: FormEvent) {
    event.preventDefault();
    const accountId = detail?.commerce?.account?.accountId;
    const parsedValue = Number(overrideValue);
    const until = new Date(overrideUntil);
    if (!accountId || !Number.isFinite(parsedValue) || parsedValue < 0 || !overrideReason.trim() || Number.isNaN(until.getTime())) return;
    const capability = create(PlanCapabilitySchema);
    if (overridePath === "cloud_device_limit") capability.cloudDeviceLimit = parsedValue;
    if (overridePath === "managed_p2p_max_concurrency") capability.managedP2pMaxConcurrency = parsedValue;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/entitlement-overrides/put",
        PutEntitlementOverrideRequestSchema,
        create(PutEntitlementOverrideRequestSchema, {
          override: create(EntitlementOverrideProjectionSchema, {
            accountId,
            capabilityMask: create(FieldMaskSchema, { paths: [overridePath] }),
            capability,
            effectiveFromUnixMillis: BigInt(Date.now()),
            effectiveUntilUnixMillis: BigInt(until.getTime()),
            reason: overrideReason.trim(),
          }),
          requestId: crypto.randomUUID(),
        }),
        PutEntitlementOverrideResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setOverrideValue("");
      setOverrideReason("");
      setOverrideUntil("");
      await select(accountId);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Entitlement override failed");
    } finally {
      setBusy(false);
    }
  }

  async function adjustSubscription(event: FormEvent) {
    event.preventDefault();
    const subscription = detail?.commerce?.subscription;
    const days = Number(adjustmentDays);
    const activeCatalog = catalogHistory?.releases.find((item) => item.active)?.catalog;
    const plan = activeCatalog?.plans.find((item) => item.planId === adjustmentPlan);
    if (!subscription || !Number.isInteger(days) || days < 1 || !adjustmentReason.trim()) return;
    if (adjustmentKind !== SubscriptionAdjustmentKind.EXTEND && !plan) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/subscriptions/adjust",
        CreateSubscriptionAdjustmentRequestSchema,
        create(CreateSubscriptionAdjustmentRequestSchema, {
          accountId: subscription.accountId,
          adjustmentKind,
          targetPlanId: adjustmentKind === SubscriptionAdjustmentKind.EXTEND ? "" : plan?.planId,
          targetPlanVersion: adjustmentKind === SubscriptionAdjustmentKind.EXTEND ? 0n : plan?.planVersion,
          durationDays: days,
          expectedSubscriptionRevision: subscription.revision,
          reason: adjustmentReason.trim(),
          requestId: crypto.randomUUID(),
        }),
        CreateSubscriptionAdjustmentResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setAdjustmentReason("");
      await select(subscription.accountId);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Subscription adjustment failed");
    } finally {
      setBusy(false);
    }
  }

  async function createPromotion(event: FormEvent) {
    event.preventDefault();
    const value = Number(promotionPercent);
    const limit = Number(promotionLimit);
    const until = new Date(promotionUntil);
    if (!promotionCode.trim() || !promotionCreemCode.trim() || !promotionReason.trim() || !Number.isFinite(value) || value <= 0 || !Number.isInteger(limit) || limit < 1 || Number.isNaN(until.getTime())) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/promotions/create",
        CreatePromotionRequestSchema,
        create(CreatePromotionRequestSchema, {
          promotion: create(PromotionProjectionSchema, {
            code: promotionCode.trim(),
            discountKind: promotionKind,
            percentBasisPoints: promotionKind === PromotionDiscountKind.PERCENT ? Math.round(value * 100) : 0,
            fixedMinor: promotionKind === PromotionDiscountKind.FIXED ? BigInt(Math.round(value)) : 0n,
            currency: promotionKind === PromotionDiscountKind.FIXED ? "USD" : "",
            planIds: [promotionPlan],
            effectiveFromUnixMillis: BigInt(Date.now()),
            effectiveUntilUnixMillis: BigInt(until.getTime()),
            maxRedemptions: limit,
            maxRedemptionsPerAccount: 1,
            creemDiscountCode: promotionCreemCode.trim(),
            reason: promotionReason.trim(),
          }),
          requestId: crypto.randomUUID(),
        }),
        CreatePromotionResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setPromotionCode("");
      setPromotionCreemCode("");
      setPromotionReason("");
      setPromotionUntil("");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Promotion creation failed");
    } finally {
      setBusy(false);
    }
  }

  async function applyPaymentEvent(orderId: string, eventType: PaymentEventType) {
    const reason = paymentReasons[orderId]?.trim();
    if (!reason) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/orders/payment-event",
        ApplyOperatorPaymentEventRequestSchema,
        create(ApplyOperatorPaymentEventRequestSchema, { orderId, eventType, reason, requestId: crypto.randomUUID() }),
        ApplyOperatorPaymentEventResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      setPaymentReasons((current) => ({ ...current, [orderId]: "" }));
      await load();
      if (detail?.commerce?.account?.accountId) await select(detail.commerce.account.accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Payment event failed");
    } finally {
      setBusy(false);
    }
  }

  async function transition(kind: SubscriptionTransitionKind) {
    const accountId = detail?.commerce?.account?.accountId;
    if (!accountId) return;
    try {
      await protoPost(
        "/api/v1/operator/subscription/transition",
        OperatorTransitionSubscriptionRequestSchema,
        create(OperatorTransitionSubscriptionRequestSchema, {
          accountId,
          transition: kind,
        }),
        OperatorTransitionSubscriptionResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      await select(accountId);
      await load();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Subscription transition failed",
      );
    }
  }

  async function revokeDevice(deviceId: string, authEpoch: bigint) {
    try {
      await protoPost(
        "/api/v1/operator/commands",
        CreateManagementCommandRequestSchema,
        create(CreateManagementCommandRequestSchema, {
          accountId: detail?.commerce?.account?.accountId,
          commandKind: ManagementCommandKind.REVOKE_CLOUD_DEVICE,
          idempotencyKey: crypto.randomUUID(),
          target: create(ManagementCommandTargetSchema, {
            target: {
              case: "cloudDevice",
              value: create(RevokeCloudDeviceTargetSchema, {
                deviceId,
                expectedAuthEpoch: authEpoch,
              }),
            },
          }),
        }),
        CreateManagementCommandResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      if (detail?.commerce?.account?.accountId)
        await select(detail.commerce.account.accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Device revoke failed");
    }
  }

  async function migrateAssignment(daemonDeviceId: string, targetHubId: string) {
    const accountId = detail?.commerce?.account?.accountId;
    if (!accountId) return;
    try {
      await protoPost(
        "/api/v1/operator/commands",
        CreateManagementCommandRequestSchema,
        create(CreateManagementCommandRequestSchema, {
          accountId,
          commandKind: ManagementCommandKind.MIGRATE_ASSIGNMENT,
          idempotencyKey: crypto.randomUUID(),
          target: create(ManagementCommandTargetSchema, {
            target: {
              case: "assignmentMigration",
              value: create(AssignmentMigrationTargetSchema, {
                daemonDeviceId,
                targetHubId,
              }),
            },
          }),
        }),
        CreateManagementCommandResponseSchema,
        "muxvia_cloud_operator_csrf",
      );
      await select(accountId);
      await load();
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Assignment migration failed",
      );
    }
  }

  async function logout() {
    await protoPost(
      "/api/v1/operator/logout",
      OperatorLogoutRequestSchema,
      create(OperatorLogoutRequestSchema),
      OperatorLogoutResponseSchema,
      "muxvia_cloud_operator_csrf",
    );
    setAuthenticated(false);
    setAccounts(undefined);
    setOrders(undefined);
    setSubscriptions(undefined);
    setPromotions(undefined);
    setFleet(undefined);
    setDetail(undefined);
  }

  if (!authenticated)
    return (
      <main className="grid min-h-dvh place-items-center bg-background p-5 text-foreground">
        <form
          className="w-full max-w-sm border border-line bg-panel p-6"
          onSubmit={login}
        >
          <ShieldCheck className="size-8 text-primary" />
          <p className="mt-6 font-mono text-[10px] text-primary">
            OPERATOR CONTROL PLANE
          </p>
          <h1 className="mt-2 text-3xl font-light">Operator sign in</h1>
          <label className="mt-6 grid gap-2 font-mono text-[9px] text-muted-foreground">
            ACCESS TOKEN
            <Input
              data-testid="operator-token"
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
            />
          </label>
          <Button
            data-testid="operator-submit"
            className="mt-4 w-full"
            disabled={!token}
          >
            Sign in
          </Button>
          {error && <p className="mt-4 text-xs text-destructive">{error}</p>}
        </form>
      </main>
    );

  return (
    <main className="min-h-dvh bg-background p-5 text-foreground md:p-10">
      <header className="flex flex-wrap items-center justify-between gap-4 border-b border-line pb-5">
        <div>
          <p className="font-mono text-[10px] text-primary">
            MUXVIA CLOUD / OPERATOR
          </p>
          <h1 className="mt-2 text-3xl font-light">Control plane</h1>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={() => void load()}>
            <RefreshCw />
          </Button>
          <Button variant="outline" onClick={logout}>
            <LogOut />
            Sign out
          </Button>
        </div>
      </header>
      {error && (
        <p role="alert" className="mt-5 border border-destructive p-3 text-xs text-destructive">
          {error}
        </p>
      )}
      <div className="mt-6 grid gap-5 xl:grid-cols-3" data-testid="operator-commerce-operations">
        <section className="border border-line bg-panel" data-testid="operator-orders">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><ReceiptText className="size-4 text-primary" /><strong className="text-sm">Orders</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{orders?.orders.length ?? 0} TOTAL</span>
          </header>
          <div className="max-h-[36rem] overflow-y-auto">
            {orders?.orders.length ? orders.orders.map((item) => {
              const order = item.order;
              if (!order) return null;
              const canCollect = order.status === OrderStatus.PENDING;
              const canReverse = order.status === OrderStatus.PAID;
              return (
                <div className="border-b border-line p-4" key={order.orderId} data-testid={`operator-order-${order.orderId}`}>
                  <div className="flex items-start justify-between gap-3">
                    <span className="min-w-0"><strong className="block truncate text-sm">{order.planId} · {formatMoney(order.totalMinor, order.price?.currency)}</strong><small className="font-mono text-[10px] text-muted-foreground">{order.orderId}</small></span>
                    <span className="text-[10px] font-semibold text-primary">{OrderStatus[order.status]}</span>
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">Account {order.accountId} · {item.paymentAttempts.length} attempts · {item.paymentEvents.length} events</p>
                  {(canCollect || canReverse) && <div className="mt-3 grid gap-2">
                    <Input aria-label={`Reason for order ${order.orderId}`} placeholder="Required operational reason" value={paymentReasons[order.orderId] ?? ""} onChange={(event) => setPaymentReasons((current) => ({ ...current, [order.orderId]: event.target.value }))} />
                    <div className="flex flex-wrap gap-2">
                      {canCollect && <Button size="sm" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.SUCCEEDED)}>Record payment</Button>}
                      {canReverse && <Button size="sm" variant="outline" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.REFUNDED)}>Refund</Button>}
                      {canReverse && <Button size="sm" variant="outline" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.REVOKED)}>Revoke</Button>}
                    </div>
                  </div>}
                  {item.paymentEvents.length > 0 && <details className="mt-3 text-xs"><summary className="cursor-pointer text-muted-foreground">Event timeline</summary><div className="mt-2 border-l border-line pl-3">{item.paymentEvents.slice().reverse().map((event) => <p className="py-1" key={event.event?.providerEventId}><strong>{PaymentEventType[event.event?.eventType ?? 0]}</strong> · {event.event?.provider} · {new Date(Number(event.event?.occurredAtUnixMillis ?? 0n)).toLocaleString()}</p>)}</div></details>}
                </div>
              );
            }) : <p className="p-4 text-xs text-muted-foreground">No orders yet.</p>}
          </div>
        </section>
        <section className="border border-line bg-panel" data-testid="operator-subscriptions">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><UserRoundCog className="size-4 text-primary" /><strong className="text-sm">Subscriptions</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{subscriptions?.subscriptions.length ?? 0} TOTAL</span>
          </header>
          <div className="max-h-[36rem] overflow-y-auto">
            {subscriptions?.subscriptions.map((subscription) => <button type="button" className="grid min-h-16 w-full grid-cols-[1fr_auto] items-center gap-3 border-b border-line p-4 text-left hover:bg-soft focus-visible:outline-2 focus-visible:outline-primary" key={subscription.subscriptionId} onClick={() => void select(subscription.accountId)}>
              <span><strong className="block text-sm">{subscription.planId} v{subscription.planVersion.toString()}</strong><small className="font-mono text-[10px] text-muted-foreground">{subscription.accountId}</small></span>
              <span className="text-[10px] font-semibold text-primary">{SubscriptionStatus[subscription.status]}</span>
            </button>)}
          </div>
        </section>
        <section className="border border-line bg-panel" data-testid="operator-promotions">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><TicketPercent className="size-4 text-primary" /><strong className="text-sm">Promotions</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{promotions?.promotions.length ?? 0} RELEASES</span>
          </header>
          <form className="grid gap-3 border-b border-line p-4" onSubmit={createPromotion}>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="grid gap-2 text-xs font-medium">Code<Input data-testid="promotion-code" value={promotionCode} onChange={(event) => setPromotionCode(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">Plan<Input value={promotionPlan} onChange={(event) => setPromotionPlan(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">Discount<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={promotionKind} onChange={(event) => setPromotionKind(Number(event.target.value) as PromotionDiscountKind)}><option value={PromotionDiscountKind.PERCENT}>Percent</option><option value={PromotionDiscountKind.FIXED}>Fixed minor units</option></select></label>
              <label className="grid gap-2 text-xs font-medium">Value<Input type="number" min="1" value={promotionPercent} onChange={(event) => setPromotionPercent(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">Total limit<Input type="number" min="1" value={promotionLimit} onChange={(event) => setPromotionLimit(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">Effective until<Input type="datetime-local" value={promotionUntil} onChange={(event) => setPromotionUntil(event.target.value)} /></label>
            </div>
            <label className="grid gap-2 text-xs font-medium">Creem discount code<Input value={promotionCreemCode} onChange={(event) => setPromotionCreemCode(event.target.value)} /></label>
            <label className="grid gap-2 text-xs font-medium">Publish reason<Input value={promotionReason} onChange={(event) => setPromotionReason(event.target.value)} /></label>
            <Button data-testid="promotion-create" disabled={busy || !promotionCode.trim() || !promotionCreemCode.trim() || !promotionReason.trim() || !promotionUntil}>Register Creem promotion</Button>
          </form>
          <div className="max-h-64 overflow-y-auto">{promotions?.promotions.map((promotion) => <div className="grid grid-cols-[1fr_auto] gap-3 border-b border-line p-4 text-xs" key={promotion.promotionId}><span><strong className="block text-sm">{promotion.code}</strong><small className="text-muted-foreground">{promotion.planIds.join(", ")} · revision {promotion.revision.toString()}</small></span><span className={promotion.state === PromotionState.ACTIVE ? "text-success" : "text-muted-foreground"}>{PromotionState[promotion.state]}</span></div>)}</div>
        </section>
      </div>
      <section className="mt-6 border border-line bg-panel" data-testid="operator-catalog">
        <header className="flex flex-wrap items-center justify-between gap-4 border-b border-line p-4">
          <div className="flex items-center gap-3">
            <PackageOpen className="size-4 text-primary" />
            <div>
              <h2 className="text-sm font-medium">Plan catalog</h2>
              <p className="mt-1 text-xs text-muted-foreground">Immutable releases for new checkout; existing subscriptions keep their purchased version.</p>
            </div>
          </div>
          <span className="font-mono text-[10px] text-muted-foreground">
            {catalogHistory?.releases.length ?? 0} RELEASES
          </span>
        </header>
        <div className="grid lg:grid-cols-[minmax(260px,0.7fr)_minmax(420px,1.3fr)]">
          <div className="border-b border-line lg:border-b-0 lg:border-r">
            {catalogHistory?.releases.map((release) => (
              <button
                type="button"
                key={release.catalog?.catalogVersion.toString()}
                className="grid min-h-16 w-full grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line px-4 py-3 text-left hover:bg-soft focus-visible:outline-2 focus-visible:outline-primary"
                onClick={() => release.catalog && showCatalog(release.catalog)}
              >
                <History className="size-4 text-muted-foreground" />
                <span>
                  <strong className="block text-sm">Catalog {release.catalog?.catalogVersion.toString()}</strong>
                  <small className="text-muted-foreground">{new Date(Number(release.publishedAtUnixMillis)).toLocaleString()}</small>
                </span>
                <span className={release.active ? "text-[10px] font-semibold text-success" : "text-[10px] text-muted-foreground"}>
                  {release.active ? "ACTIVE" : "HISTORY"}
                </span>
              </button>
            ))}
          </div>
          <form className="grid gap-4 p-4" onSubmit={publishCatalog}>
            <label className="grid gap-2 text-xs font-medium">
              Versioned Proto JSON
              <textarea
                ref={catalogEditorRef}
                data-testid="catalog-editor"
                className="min-h-64 w-full resize-y border border-line-strong bg-background p-3 font-mono text-xs leading-5 outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
                value={catalogDraft}
                onChange={(event) => setCatalogDraft(event.target.value)}
                spellCheck={false}
              />
            </label>
            <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
              <label className="grid gap-2 text-xs font-medium">
                Publish reason
                <Input data-testid="catalog-reason" value={catalogReason} onChange={(event) => setCatalogReason(event.target.value)} placeholder="Describe price or capability changes" />
              </label>
              <Button data-testid="catalog-publish" disabled={busy || !catalogDraft || !catalogReason.trim()}>
                Publish release
              </Button>
            </div>
          </form>
        </div>
      </section>
      <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(360px,0.8fr)_minmax(480px,1.2fr)]">
        <section className="border border-line bg-panel">
          <header className="flex items-center gap-2 border-b border-line p-4">
            <Search className="size-4" />
            <Input
              value={query}
              placeholder="Search account or email"
              onChange={(event) => setQuery(event.target.value)}
            />
            <Button onClick={() => void load(query)}>Search</Button>
          </header>
          {accounts?.accounts.map((item) => (
            <button
              className="grid w-full gap-2 border-b border-line p-4 text-left hover:bg-soft"
              key={item.account?.accountId}
              data-testid={`operator-account-${item.account?.accountId}`}
              onClick={() =>
                item.account && void select(item.account.accountId)
              }
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                <Building2 className="size-4" />
                {item.account?.email}
              </span>
              <span className="text-xs text-muted-foreground">
                {item.subscription?.planId} /{" "}
                {SubscriptionStatus[item.subscription?.status ?? 0]} /{" "}
                {item.relayQuota?.usedBytes ?? 0n} bytes
              </span>
            </button>
          ))}
        </section>
        <section className="border border-line bg-panel">
          {detail ? (
            <>
              <header className="border-b border-line p-5">
                <p className="font-mono text-[9px] text-muted-foreground">
                  ACCOUNT
                </p>
                <h2 className="mt-2 text-xl">
                  {detail.commerce?.account?.email}
                </h2>
                <p className="mt-1 font-mono text-[10px] text-muted-foreground">
                  {detail.commerce?.account?.accountId}
                </p>
                <div className="mt-4 flex gap-2">
                  <Button
                    variant="outline"
                    data-testid="operator-suspend"
                    onClick={() =>
                      void transition(SubscriptionTransitionKind.SUSPEND)
                    }
                  >
                    Suspend
                  </Button>
                  <Button
                    variant="outline"
                    data-testid="operator-restore"
                    onClick={() =>
                      void transition(SubscriptionTransitionKind.RESTORE)
                    }
                  >
                    Restore
                  </Button>
                </div>
              </header>
              <div className="grid gap-4 p-5 md:grid-cols-2">
                <Stat
                  label="Plan"
                  value={detail.commerce?.subscription?.planId ?? "-"}
                />
                <Stat
                  label="Subscription"
                  value={
                    SubscriptionStatus[
                      detail.commerce?.subscription?.status ?? 0
                    ]
                  }
                />
                <Stat
                  label="Devices"
                  value={String(detail.devices?.devices.length ?? 0)}
                />
                <Stat
                  label="Sessions"
                  value={String(detail.topology?.peerSessions.length ?? 0)}
                />
              </div>
              <div className="border-t border-line" data-testid="operator-subscription-adjustment">
                <header className="flex items-center gap-2 p-4"><UserRoundCog className="size-4 text-primary" /><div><h3 className="text-sm font-medium">Subscription adjustment</h3><p className="mt-1 text-xs text-muted-foreground">Grant, extend, or change service without creating a paid order.</p></div></header>
                <form className="grid gap-3 border-t border-line p-4 md:grid-cols-2" onSubmit={adjustSubscription}>
                  <label className="grid gap-2 text-xs font-medium">Adjustment<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={adjustmentKind} onChange={(event) => setAdjustmentKind(Number(event.target.value) as SubscriptionAdjustmentKind)}><option value={SubscriptionAdjustmentKind.GRANT}>Grant</option><option value={SubscriptionAdjustmentKind.EXTEND}>Extend</option><option value={SubscriptionAdjustmentKind.CHANGE_PLAN}>Change plan</option></select></label>
                  <label className="grid gap-2 text-xs font-medium">Duration days<Input type="number" min="1" value={adjustmentDays} onChange={(event) => setAdjustmentDays(event.target.value)} /></label>
                  {adjustmentKind !== SubscriptionAdjustmentKind.EXTEND && <label className="grid gap-2 text-xs font-medium">Target plan<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={adjustmentPlan} onChange={(event) => setAdjustmentPlan(event.target.value)}>{catalogHistory?.releases.find((item) => item.active)?.catalog?.plans.filter((plan) => !plan.included).map((plan) => <option value={plan.planId} key={plan.planId}>{plan.presentation?.name || plan.planId}</option>)}</select></label>}
                  <label className="grid gap-2 text-xs font-medium">Reason<Input data-testid="adjustment-reason" value={adjustmentReason} onChange={(event) => setAdjustmentReason(event.target.value)} /></label>
                  <Button data-testid="adjustment-create" className="md:col-start-2" disabled={busy || !adjustmentReason.trim() || Number(adjustmentDays) < 1}>Apply adjustment</Button>
                </form>
                <div className="border-t border-line">{detail.commerce?.subscriptionAdjustments.length ? detail.commerce.subscriptionAdjustments.map((item) => <div className="grid grid-cols-[1fr_auto] gap-3 border-b border-line px-4 py-3 text-xs" key={item.adjustmentId}><span><strong className="block">{SubscriptionAdjustmentKind[item.adjustmentKind]} · {item.durationDays} days</strong><small className="text-muted-foreground">{item.reason} · {item.actorId}</small></span><span className="font-mono text-[10px] text-muted-foreground">REV {item.resultingSubscriptionRevision.toString()}</span></div>) : <p className="p-4 text-xs text-muted-foreground">No manual adjustments.</p>}</div>
              </div>
              <div className="border-t border-line" data-testid="operator-overrides">
                <header className="flex items-center gap-2 p-4">
                  <SlidersHorizontal className="size-4 text-primary" />
                  <div>
                    <h3 className="text-sm font-medium">User privileges</h3>
                    <p className="mt-1 text-xs text-muted-foreground">Temporary typed capability overrides. Expiry automatically restores the subscribed plan.</p>
                  </div>
                </header>
                <form className="grid gap-3 border-t border-line p-4 md:grid-cols-2" onSubmit={putOverride}>
                  <label className="grid gap-2 text-xs font-medium">
                    Capability
                    <select className="min-h-11 border border-line-strong bg-background px-3 text-sm outline-none focus:border-primary" value={overridePath} onChange={(event) => setOverridePath(event.target.value)}>
                      <option value="cloud_device_limit">Cloud device limit</option>
                      <option value="managed_p2p_max_concurrency">Managed P2P concurrency</option>
                    </select>
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    Value
                    <Input data-testid="override-value" type="number" min="0" value={overrideValue} onChange={(event) => setOverrideValue(event.target.value)} />
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    Effective until
                    <Input data-testid="override-until" type="datetime-local" value={overrideUntil} onChange={(event) => setOverrideUntil(event.target.value)} />
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    Reason
                    <Input data-testid="override-reason" value={overrideReason} onChange={(event) => setOverrideReason(event.target.value)} placeholder="Support grant or incident response" />
                  </label>
                  <Button className="md:col-start-2" data-testid="override-create" disabled={busy || !overrideValue || !overrideUntil || !overrideReason.trim()}>
                    Apply privilege
                  </Button>
                </form>
                <div className="border-t border-line">
                  {overrides?.overrides.length ? overrides.overrides.slice().reverse().map((item) => (
                    <div className="grid gap-2 border-b border-line px-4 py-3 text-xs md:grid-cols-[1fr_auto]" key={item.overrideId}>
                      <span>
                        <strong className="block">{item.capabilityMask?.paths.join(", ")}</strong>
                        <small className="text-muted-foreground">{item.reason} · revision {item.revision.toString()}</small>
                      </span>
                      <span className={item.revokedAtUnixMillis > 0n || item.effectiveUntilUnixMillis <= BigInt(Date.now()) ? "text-muted-foreground" : "text-success"}>
                        {item.revokedAtUnixMillis > 0n ? "REVOKED" : item.effectiveUntilUnixMillis <= BigInt(Date.now()) ? "EXPIRED" : "ACTIVE"}
                      </span>
                    </div>
                  )) : <p className="p-4 text-xs text-muted-foreground">No privilege overrides for this account.</p>}
                </div>
              </div>
              <div className="border-t border-line">
                <h3 className="p-4 text-sm font-medium">Devices</h3>
                {detail.devices?.devices.map((device) => {
                  const presence = detail.topology?.presences.find(
                    (item) => item.daemonDeviceId === device.deviceId,
                  );
                  const currentHubId =
                    device.assignedHubId || presence?.controlOwnerHubId;
                  return (
                    <div
                      className="grid gap-3 border-t border-line px-4 py-3 text-xs md:grid-cols-[minmax(0,1fr)_auto]"
                      key={device.deviceId}
                      data-testid={`operator-device-${device.deviceId}`}
                    >
                      <span className="min-w-0">
                        <strong className="block truncate">
                          {device.displayName || device.deviceId}
                        </strong>
                        <small className="font-mono text-muted-foreground">
                          {device.deviceId}
                          {currentHubId
                            ? ` / ${currentHubId}`
                            : ""}
                        </small>
                      </span>
                      <div className="flex flex-wrap justify-end gap-2">
                        {device.deviceKind ===
                          ManagedDeviceKind.DAEMON &&
                          fleet?.hubs
                            .filter(
                              (hub) =>
                                hub.hubReady &&
                                hub.deployment?.hubId &&
                                hub.deployment.hubId !== currentHubId,
                            )
                            .map((hub) => (
                              <Button
                                key={hub.deployment?.hubId}
                                variant="outline"
                                data-testid={`migrate-${device.deviceId}-${hub.deployment?.hubId}`}
                                onClick={() =>
                                  void migrateAssignment(
                                    device.deviceId,
                                    hub.deployment?.hubId ?? "",
                                  )
                                }
                              >
                                Move to {hub.deployment?.publicLabel || hub.deployment?.hubId}
                              </Button>
                            ))}
                        <Button
                          variant="outline"
                          disabled={device.revoked}
                          data-testid={`revoke-${device.deviceId}`}
                          onClick={() =>
                            void revokeDevice(device.deviceId, device.authEpoch)
                          }
                        >
                          Revoke
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
              <div className="border-t border-line">
                <h3 className="p-4 text-sm font-medium">
                  Audit and payment events
                </h3>
                {detail.commerce?.audit
                  .slice(-8)
                  .reverse()
                  .map((item) => (
                    <div
                      className="grid grid-cols-[180px_1fr] border-t border-line px-4 py-3 text-xs"
                      key={item.auditId}
                    >
                      <span className="text-muted-foreground">
                        {new Date(
                          Number(item.occurredAtUnixMillis),
                        ).toLocaleString()}
                      </span>
                      <span>{item.action}</span>
                    </div>
                  ))}
              </div>
            </>
          ) : (
            <p className="p-8 text-sm text-muted-foreground">
              Select an account.
            </p>
          )}
        </section>
      </div>
      <section className="mt-5 border border-line bg-panel">
        <h2 className="flex items-center gap-2 border-b border-line p-4 text-sm font-medium">
          <Server className="size-4" />
          Hub and Relay fleet
        </h2>
        <div className="grid gap-3 p-4 lg:grid-cols-2">
          {fleet?.hubs.map((hub) => (
            <article
              className="border border-line p-4"
              key={hub.deployment?.hubId}
            >
              <div className="flex items-center justify-between">
                <strong>
                  {hub.deployment?.publicLabel || hub.deployment?.hubId}
                </strong>
                <span
                  className={
                    hub.freshness === Freshness.FRESH
                      ? "text-success"
                      : "text-warning"
                  }
                >
                  {Freshness[hub.freshness]}
                </span>
              </div>
              <p className="mt-3 font-mono text-[10px] text-muted-foreground">
                Hub gen {hub.hubControlGeneration} / Relay gen{" "}
                {hub.relayControlGeneration} / Projection{" "}
                {hub.projectionRevision}
              </p>
              <p className="mt-2 text-xs">
                Hub {hub.hubReady ? "ready" : "not ready"} · Relay{" "}
                {hub.relayReady ? "ready" : "not ready"}
              </p>
            </article>
          ))}
        </div>
      </section>
    </main>
  );
}

function decodeToken(value: string): Uint8Array {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(
    normalized + "=".repeat((4 - (normalized.length % 4)) % 4),
  );
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}
function formatMoney(minor: bigint, currency = "USD") {
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(Number(minor) / 100);
}
function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="border border-line p-4">
      <p className="font-mono text-[9px] text-muted-foreground">
        {label.toUpperCase()}
      </p>
      <strong className="mt-2 block text-lg font-light">{value}</strong>
    </div>
  );
}
