import { create, fromJsonString, toJsonString, type DescMessage, type MessageShape } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  Building2,
  Check,
  Edit3,
  Laptop,
  KeyRound,
  History,
  Menu,
  PackageOpen,
  Rocket,
  PauseCircle,
  Plus,
  Power,
  ReceiptText,
  RefreshCw,
  Radio,
  Search,
  Server,
  SlidersHorizontal,
  TicketPercent,
  UserRoundCog,
  X,
} from "lucide-react";
import { FormEvent, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import {
  GetOperatorAccountRequestSchema,
  GetOperatorAccountResponseSchema,
  ListHubFleetRequestSchema,
  ListHubFleetResponseSchema,
  CreateHubDeploymentRequestSchema,
  CreateHubDeploymentResponseSchema,
  UpdateHubDeploymentRequestSchema,
  UpdateHubDeploymentResponseSchema,
  ApproveHubDeploymentIdentityRequestSchema,
  ApproveHubDeploymentIdentityResponseSchema,
  SetHubDeploymentDrainRequestSchema,
  SetHubDeploymentDrainResponseSchema,
  DisableHubDeploymentRequestSchema,
  DisableHubDeploymentResponseSchema,
  ListOperatorAccountsRequestSchema,
  ListOperatorAccountsResponseSchema,
  ListOperatorAgentsRequestSchema,
  ListOperatorAgentsResponseSchema,
  ListOperatorOrdersRequestSchema,
  ListOperatorOrdersResponseSchema,
  ListOperatorSubscriptionsRequestSchema,
  ListOperatorSubscriptionsResponseSchema,
  CreateSubscriptionAdjustmentRequestSchema,
  CreateSubscriptionAdjustmentResponseSchema,
  ApplyOperatorPaymentEventRequestSchema,
  ApplyOperatorPaymentEventResponseSchema,
  ReconcileCreemPaymentAttemptRequestSchema,
  ReconcileCreemPaymentAttemptResponseSchema,
  CreatePromotionRequestSchema,
  CreatePromotionResponseSchema,
  DisablePromotionRequestSchema,
  DisablePromotionResponseSchema,
  ListPromotionsRequestSchema,
  ListPromotionsResponseSchema,
  ListPromotionRedemptionsRequestSchema,
  ListPromotionRedemptionsResponseSchema,
  ListPlanCatalogReleasesRequestSchema,
  ListPlanCatalogReleasesResponseSchema,
  ListEntitlementOverridesRequestSchema,
  ListEntitlementOverridesResponseSchema,
  PublishPlanCatalogRequestSchema,
  PublishPlanCatalogResponseSchema,
  PutEntitlementOverrideRequestSchema,
  PutEntitlementOverrideResponseSchema,
  RevokeEntitlementOverrideRequestSchema,
  RevokeEntitlementOverrideResponseSchema,
  ManagementActorKind,
  GetOperatorWorkspaceResponseSchema,
  OperatorWorkspaceModule,
  RecentAuthenticationRequestSchema,
  RecentAuthenticationResponseSchema,
  OperatorTransitionSubscriptionRequestSchema,
  OperatorTransitionSubscriptionResponseSchema,
  PageRequestSchema,
  CreateManagementCommandRequestSchema,
  CreateManagementCommandResponseSchema,
  ManagementCommandKind,
  ManagementCommandTargetSchema,
  AssignmentMigrationTargetSchema,
  RevokeCloudDeviceTargetSchema,
  RevokeOperatorAccountSessionRequestSchema,
  RevokeOperatorAccountSessionResponseSchema,
  ListReleaseArtifactsRequestSchema,
  ListReleaseArtifactsResponseSchema,
  PublishReleaseArtifactRequestSchema,
  PublishReleaseArtifactResponseSchema,
  ReleaseArtifactProjectionSchema,
  SetReleaseChannelRequestSchema,
  SetReleaseChannelResponseSchema,
  CommandAuthorityResult,
  CommandDeliveryState,
  CommandExecutionState,
  CommandObservedEffect,
  type GetOperatorAccountResponse,
  type ListHubFleetResponse,
  type ListOperatorAccountsResponse,
  type ListOperatorAgentsResponse,
  type ListOperatorOrdersResponse,
  type ListOperatorSubscriptionsResponse,
  type ListPromotionsResponse,
  type ListPromotionRedemptionsResponse,
  type ListPlanCatalogReleasesResponse,
  type ListEntitlementOverridesResponse,
  type ListReleaseArtifactsResponse,
} from "@/generated/cloudpb/cloud_management_pb";
import { KickPresenceTargetSchema } from "@/generated/cloudpb/cloud_hub_control_pb";
import {
  EntitlementOverrideProjectionSchema,
  PromotionProjectionSchema,
  OrderStatus,
  PaymentAttemptStatus,
  PaymentEventType,
  PromotionDiscountKind,
  PromotionState,
  PromotionRedemptionState,
  PlanCapabilitySchema,
  PlanCatalogContractSchema,
  SubscriptionStatus,
  SubscriptionAdjustmentKind,
  SubscriptionTransitionKind,
  type PlanCatalogContract,
} from "@/generated/cloudpb/cloud_product_pb";
import {
  Availability,
  Freshness,
  ManagedDeviceKind,
} from "@/generated/cloudpb/cloud_topology_pb";
import { ProtoHTTPError, protoGet, protoPost } from "@/protoApi";

type HubFormState = {
  hubId: string;
  edgeDeploymentId: string;
  relayId: string;
  region: string;
  publicLabel: string;
  publicHubUrl: string;
  healthUrl: string;
  maxAssignments: string;
  hubControlPublicKey: string;
  relayControlPublicKey: string;
  reason: string;
};

const emptyHubForm: HubFormState = {
  hubId: "", edgeDeploymentId: "", relayId: "", region: "", publicLabel: "", publicHubUrl: "", healthUrl: "", maxAssignments: "1000", hubControlPublicKey: "", relayControlPublicKey: "", reason: "",
};

type OperatorModuleKey = "users" | "agents" | "orders" | "subscriptions" | "plans" | "privileges" | "promotions" | "hubs" | "releases";

const operatorModules: ReadonlyArray<{
  key: OperatorModuleKey;
  permission: OperatorWorkspaceModule;
  icon: typeof UserRoundCog;
}> = [
  { key: "users", permission: OperatorWorkspaceModule.USERS, icon: UserRoundCog },
  { key: "agents", permission: OperatorWorkspaceModule.AGENTS, icon: Laptop },
  { key: "orders", permission: OperatorWorkspaceModule.ORDERS, icon: ReceiptText },
  { key: "subscriptions", permission: OperatorWorkspaceModule.SUBSCRIPTIONS, icon: KeyRound },
  { key: "plans", permission: OperatorWorkspaceModule.PLANS, icon: PackageOpen },
  { key: "privileges", permission: OperatorWorkspaceModule.PRIVILEGES, icon: SlidersHorizontal },
  { key: "promotions", permission: OperatorWorkspaceModule.PROMOTIONS, icon: TicketPercent },
  { key: "hubs", permission: OperatorWorkspaceModule.HUBS, icon: Server },
  { key: "releases", permission: OperatorWorkspaceModule.RELEASES, icon: Rocket },
];

const orderStatuses = [OrderStatus.PENDING, OrderStatus.PAID, OrderStatus.PAYMENT_FAILED, OrderStatus.REFUNDED, OrderStatus.REVOKED];
const subscriptionStatuses = [SubscriptionStatus.PENDING, SubscriptionStatus.ACTIVE, SubscriptionStatus.CANCEL_AT_PERIOD_END, SubscriptionStatus.CANCELED, SubscriptionStatus.SUSPENDED, SubscriptionStatus.EXPIRED, SubscriptionStatus.TRIALING, SubscriptionStatus.GRACE, SubscriptionStatus.PAST_DUE];

function moduleFromPath(pathname = window.location.pathname): OperatorModuleKey | undefined {
  const key = pathname.replace(/\/$/, "").split("/")[2];
  if (key === "catalog") return "plans";
  return operatorModules.some((module) => module.key === key) ? key as OperatorModuleKey : undefined;
}

export default function OperatorPage() {
  const { t, i18n } = useTranslation();
  const [workspaceLoaded, setWorkspaceLoaded] = useState(false);
  const [workspaceModules, setWorkspaceModules] = useState<OperatorWorkspaceModule[]>([]);
  const [activeModule, setActiveModule] = useState<OperatorModuleKey | undefined>(() => moduleFromPath());
  const [routePath, setRoutePath] = useState(window.location.pathname);
  const [navigationOpen, setNavigationOpen] = useState(false);
  const navigationRef = useRef<HTMLElement>(null);
  const navigationTriggerRef = useRef<HTMLButtonElement>(null);
  const [loaded, setLoaded] = useState(false);
  const [reauthPassword, setReauthPassword] = useState("");
  const [reauthExpiresAt, setReauthExpiresAt] = useState(0);
  const reauthExpiresAtRef = useRef(0);
  const [pendingMutation, setPendingMutation] = useState<{ label: string; replay: () => void; returnFocus?: HTMLElement }>();
  const [accounts, setAccounts] = useState<ListOperatorAccountsResponse>();
  const [agents, setAgents] = useState<ListOperatorAgentsResponse>();
  const [orders, setOrders] = useState<ListOperatorOrdersResponse>();
  const [subscriptions, setSubscriptions] = useState<ListOperatorSubscriptionsResponse>();
  const [promotions, setPromotions] = useState<ListPromotionsResponse>();
  const [promotionRedemptions, setPromotionRedemptions] = useState<ListPromotionRedemptionsResponse>();
  const [fleet, setFleet] = useState<ListHubFleetResponse>();
  const [releases, setReleases] = useState<ListReleaseArtifactsResponse>();
  const [detail, setDetail] = useState<GetOperatorAccountResponse>();
  const [selectedAccountId, setSelectedAccountId] = useState("");
  const [catalogHistory, setCatalogHistory] =
    useState<ListPlanCatalogReleasesResponse>();
  const [overrides, setOverrides] =
    useState<ListEntitlementOverridesResponse>();
  const [query, setQuery] = useState("");
  const [accountSubscriptionStatus, setAccountSubscriptionStatus] = useState<SubscriptionStatus>(SubscriptionStatus.UNSPECIFIED);
  const [agentFreshness, setAgentFreshness] = useState<Freshness>(Freshness.UNSPECIFIED);
  const [orderAccountId, setOrderAccountId] = useState("");
  const [orderProvider, setOrderProvider] = useState("");
  const [orderStatus, setOrderStatus] = useState<OrderStatus>(OrderStatus.UNSPECIFIED);
  const [subscriptionStatus, setSubscriptionStatus] = useState<SubscriptionStatus>(SubscriptionStatus.UNSPECIFIED);
  const [directoryView, setDirectoryView] = useState<"users" | "agents">("users");
  const [error, setError] = useState("");
  const [catalogDraft, setCatalogDraft] = useState("");
  const [catalogReason, setCatalogReason] = useState("");
  const [overridePath, setOverridePath] = useState("cloud_device_limit");
  const [overrideValue, setOverrideValue] = useState("");
  const [overrideReason, setOverrideReason] = useState("");
  const [overrideUntil, setOverrideUntil] = useState("");
  const [overrideRevokeReasons, setOverrideRevokeReasons] = useState<Record<string, string>>({});
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
  const [promotionDisableReason, setPromotionDisableReason] = useState("");
  const [paymentReasons, setPaymentReasons] = useState<Record<string, string>>({});
  const [reconcileReasons, setReconcileReasons] = useState<Record<string, string>>({});
  const [hubForm, setHubForm] = useState<HubFormState>(emptyHubForm);
  const [releaseDraft, setReleaseDraft] = useState("");
  const [releaseReason, setReleaseReason] = useState("");
  const [editingHubId, setEditingHubId] = useState("");
  const [hubEdit, setHubEdit] = useState<Pick<HubFormState, "region" | "publicLabel" | "publicHubUrl" | "healthUrl" | "maxAssignments" | "reason">>({ region: "", publicLabel: "", publicHubUrl: "", healthUrl: "", maxAssignments: "", reason: "" });
  const [busy, setBusy] = useState(false);
  const loadedModulesRef = useRef(new Set<OperatorModuleKey>());
  const catalogEditorRef = useRef<HTMLTextAreaElement>(null);

  function showCatalog(catalog: PlanCatalogContract) {
    setCatalogDraft(toJsonString(PlanCatalogContractSchema, catalog, { prettySpaces: 2 }));
    requestAnimationFrame(() => {
      catalogEditorRef.current?.setSelectionRange(0, 0);
      catalogEditorRef.current?.scrollTo({ top: 0 });
    });
  }

  function updateCatalog(mutator: (catalog: PlanCatalogContract) => void) {
    try {
      const catalog = fromJsonString(PlanCatalogContractSchema, catalogDraft);
      mutator(catalog);
      setCatalogDraft(toJsonString(PlanCatalogContractSchema, catalog, { prettySpaces: 2 }));
      setError("");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Catalog draft is invalid");
    }
  }

  function updateCatalogPlan(planId: string, mutator: (plan: PlanCatalogContract["plans"][number]) => void) {
    updateCatalog((catalog) => {
      const plan = catalog.plans.find((item) => item.planId === planId);
      if (plan) mutator(plan);
    });
  }

  function navigate(module: OperatorModuleKey, replace = false) {
    const path = `/operator/${module}`;
    window.history[replace ? "replaceState" : "pushState"]({}, "", path);
    setActiveModule(module);
    setRoutePath(path);
    setDetail(undefined);
    setSelectedAccountId("");
    setNavigationOpen(false);
  }

  function navigateDetail(module: OperatorModuleKey, resourceId: string, accountId?: string) {
    const path = `/operator/${module === "plans" ? "catalog" : module}/${encodeURIComponent(resourceId)}`;
    window.history.pushState({}, "", path);
    setRoutePath(path);
    setNavigationOpen(false);
    if (accountId) void select(accountId);
  }

  async function load(search = query, module = activeModule, background = module ? loadedModulesRef.current.has(module) : false) {
    if (!module) return;
    if (!background) setLoaded(false);
    setError("");
    try {
      const page = create(PageRequestSchema, { pageSize: 100 });
      if (module === "users" || module === "privileges") {
        const nextAccounts = await protoPost(
          "/api/v1/operator/accounts/list",
          ListOperatorAccountsRequestSchema,
          create(ListOperatorAccountsRequestSchema, { query: search, subscriptionStatus: accountSubscriptionStatus, page }),
          ListOperatorAccountsResponseSchema,
          "muxvia_cloud_csrf",
        );
        setAccounts(nextAccounts);
      } else if (module === "agents") {
        const [nextAgents, nextFleet] = await Promise.all([protoPost(
          "/api/v1/operator/agents/list",
          ListOperatorAgentsRequestSchema,
          create(ListOperatorAgentsRequestSchema, { query: search, freshness: agentFreshness, includeRevoked: true, page }),
          ListOperatorAgentsResponseSchema,
          "muxvia_cloud_csrf",
        ), protoPost(
          "/api/v1/operator/fleet/list",
          ListHubFleetRequestSchema,
          create(ListHubFleetRequestSchema, { page }),
          ListHubFleetResponseSchema,
          "muxvia_cloud_csrf",
        )]);
        setAgents(nextAgents);
        setFleet(nextFleet);
      } else if (module === "hubs") {
        setFleet(await protoPost(
          "/api/v1/operator/fleet/list",
          ListHubFleetRequestSchema,
          create(ListHubFleetRequestSchema, { page }),
          ListHubFleetResponseSchema,
          "muxvia_cloud_csrf",
        ));
      } else if (module === "plans") {
        const nextCatalogHistory = await protoPost(
          "/api/v1/operator/catalog/list",
          ListPlanCatalogReleasesRequestSchema,
          create(ListPlanCatalogReleasesRequestSchema, { page }),
          ListPlanCatalogReleasesResponseSchema,
          "muxvia_cloud_csrf",
        );
        setCatalogHistory(nextCatalogHistory);
        const active = nextCatalogHistory.releases.find((item) => item.active);
        if (active?.catalog && !catalogDraft) showCatalog(active.catalog);
      } else if (module === "orders") {
        setOrders(await protoPost(
          "/api/v1/operator/orders/list",
          ListOperatorOrdersRequestSchema,
          create(ListOperatorOrdersRequestSchema, { accountId: orderAccountId.trim(), status: orderStatus, provider: orderProvider.trim(), page }),
          ListOperatorOrdersResponseSchema,
          "muxvia_cloud_csrf",
        ));
      } else if (module === "subscriptions") {
        const [nextSubscriptions, nextCatalogHistory] = await Promise.all([protoPost(
          "/api/v1/operator/subscriptions/list",
          ListOperatorSubscriptionsRequestSchema,
          create(ListOperatorSubscriptionsRequestSchema, { status: subscriptionStatus, page }),
          ListOperatorSubscriptionsResponseSchema,
          "muxvia_cloud_csrf",
        ), protoPost(
          "/api/v1/operator/catalog/list",
          ListPlanCatalogReleasesRequestSchema,
          create(ListPlanCatalogReleasesRequestSchema, { page }),
          ListPlanCatalogReleasesResponseSchema,
          "muxvia_cloud_csrf",
        )]);
        setSubscriptions(nextSubscriptions);
        setCatalogHistory(nextCatalogHistory);
      } else if (module === "promotions") {
        setPromotions(await protoPost(
          "/api/v1/operator/promotions/list",
          ListPromotionsRequestSchema,
          create(ListPromotionsRequestSchema, { includeDisabled: true, page }),
          ListPromotionsResponseSchema,
          "muxvia_cloud_csrf",
        ));
      } else if (module === "releases") {
        setReleases(await protoPost("/api/v1/operator/releases/list", ListReleaseArtifactsRequestSchema, create(ListReleaseArtifactsRequestSchema, { page }), ListReleaseArtifactsResponseSchema, "muxvia_cloud_csrf"));
      }
      loadedModulesRef.current.add(module);
      setLoaded(true);
    } catch (cause) {
      if (cause instanceof ProtoHTTPError && cause.status === 401) {
        location.href = "/login";
        return;
      }
      if (cause instanceof ProtoHTTPError && cause.status === 403) {
        location.href = "/account";
        return;
      }
      setError(cause instanceof Error ? cause.message : "Operator request failed");
      setLoaded(true);
    }
  }

  useEffect(() => {
    const onPopState = () => {
      setActiveModule(moduleFromPath());
      setRoutePath(window.location.pathname);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    if (!navigationOpen) return;
    const previousOverflow = document.body.style.overflow;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setNavigationOpen(false);
        return;
      }
      if (event.key !== "Tab" || !navigationRef.current) return;
      const focusable = Array.from(navigationRef.current.querySelectorAll<HTMLElement>("button:not([disabled]), a[href], [tabindex]:not([tabindex='-1'])"));
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", onKeyDown);
    requestAnimationFrame(() => {
      const navigation = navigationRef.current;
      (navigation?.querySelector<HTMLElement>("a[aria-current='page']") ?? navigation?.querySelector<HTMLElement>("a[href], button"))?.focus();
    });
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", onKeyDown);
      navigationTriggerRef.current?.focus();
    };
  }, [navigationOpen]);

  useEffect(() => {
    if (!pendingMutation) return;
    const returnFocus = pendingMutation.returnFocus;
    const dialog = document.querySelector<HTMLElement>("[data-testid='operator-reauth-dialog']");
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setPendingMutation(undefined);
        return;
      }
      if (event.key !== "Tab" || !dialog) return;
      const focusable = Array.from(dialog.querySelectorAll<HTMLElement>("button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex='-1'])"));
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      returnFocus?.focus();
    };
  }, [pendingMutation]);

  useEffect(() => {
    if (reauthExpiresAt <= Date.now()) return;
    const timeout = window.setTimeout(() => setReauthExpiresAt(0), reauthExpiresAt - Date.now());
    return () => window.clearTimeout(timeout);
  }, [reauthExpiresAt]);

  useEffect(() => {
    void (async () => {
      try {
        const workspace = await protoGet("/api/v1/operator/workspace", GetOperatorWorkspaceResponseSchema);
        if (workspace.modules.length === 0) {
          location.href = "/account";
          return;
        }
        setWorkspaceModules(workspace.modules);
        setWorkspaceLoaded(true);
      } catch (cause) {
        if (cause instanceof ProtoHTTPError && cause.status === 401) location.href = "/login";
        else location.href = "/account";
      }
    })();
  }, []);

  useEffect(() => {
    if (!workspaceLoaded) return;
    const allowed = operatorModules.filter((module) => workspaceModules.includes(module.permission));
    const current = allowed.find((module) => module.key === activeModule);
    if (!current) {
      if (allowed[0]) navigate(allowed[0].key, true);
      return;
    }
    setDirectoryView(current.key === "agents" ? "agents" : "users");
    setLoaded(loadedModulesRef.current.has(current.key));
    void load("", current.key, loadedModulesRef.current.has(current.key));
  }, [activeModule, workspaceLoaded]);

  useEffect(() => {
    if (!loaded || !activeModule) return;
    const resourceId = routePath.replace(/\/$/, "").split("/")[3];
    if (!resourceId) return;
    const decoded = decodeURIComponent(resourceId);
    if (activeModule === "plans") {
      const catalog = catalogHistory?.releases.find((release) => release.catalog?.catalogVersion.toString() === decoded)?.catalog;
      if (catalog) showCatalog(catalog);
      return;
    }
    if (activeModule === "promotions") {
      void loadPromotionRedemptions(decoded);
      return;
    }
    if (activeModule === "orders" || activeModule === "hubs" || activeModule === "releases") return;
    let accountId = decoded;
    if (activeModule === "agents") {
      accountId = agents?.agents.find((agent) => agent.device?.deviceId === decoded)?.account?.accountId ?? "";
    } else if (activeModule === "subscriptions") {
      accountId = subscriptions?.subscriptions.find((subscription) => subscription.subscriptionId === decoded)?.accountId ?? "";
    }
    if (accountId && accountId !== selectedAccountId) void select(accountId);
  }, [routePath, loaded, activeModule, agents, subscriptions, catalogHistory]);

  async function reauthenticate(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const response = await protoPost(
        "/api/v1/operator/reauth",
        RecentAuthenticationRequestSchema,
        create(RecentAuthenticationRequestSchema, { password: reauthPassword }),
        RecentAuthenticationResponseSchema,
        "muxvia_cloud_csrf",
      );
      setReauthPassword("");
      const expiresAt = Number(response.expiresAtUnixMillis);
      reauthExpiresAtRef.current = expiresAt;
      setReauthExpiresAt(expiresAt);
      const replay = pendingMutation?.replay;
      setPendingMutation(undefined);
      if (replay) requestAnimationFrame(replay);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Password confirmation failed");
    }
  }

  function protectMutation(event: ReactMouseEvent<HTMLElement>) {
    if (reauthExpiresAtRef.current > Date.now()) return;
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-requires-recent-auth]");
    if (!button || button.disabled) return;
    event.preventDefault();
    event.stopPropagation();
    setPendingMutation({ label: button.textContent?.trim() || t("operator.reauth.genericAction"), replay: () => button.click(), returnFocus: button });
  }

  function protectMutationSubmit(event: FormEvent<HTMLElement>) {
    if (reauthExpiresAtRef.current > Date.now()) return;
    const form = event.target as HTMLFormElement;
    const submitter = (event.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    if (!form.matches("form[data-requires-recent-auth]") && !submitter?.matches("button[data-requires-recent-auth]")) return;
    event.preventDefault();
    event.stopPropagation();
    setPendingMutation({
      label: submitter?.textContent?.trim() || t("operator.reauth.genericAction"),
      replay: () => form.requestSubmit(submitter ?? undefined),
      returnFocus: submitter ?? form,
    });
  }

  async function select(accountId: string) {
    try {
      const nextDetail = await protoPost(
          "/api/v1/operator/accounts/get",
          GetOperatorAccountRequestSchema,
          create(GetOperatorAccountRequestSchema, { accountId }),
          GetOperatorAccountResponseSchema,
          "muxvia_cloud_csrf",
        );
      setDetail(nextDetail);
      setSelectedAccountId(accountId);
      if (activeModule === "privileges") {
        const page = create(PageRequestSchema, { pageSize: 100 });
        setOverrides(await protoPost(
          "/api/v1/operator/entitlement-overrides/list",
          ListEntitlementOverridesRequestSchema,
          create(ListEntitlementOverridesRequestSchema, {
            accountId,
            includeRevoked: true,
            page,
          }),
          ListEntitlementOverridesResponseSchema,
          "muxvia_cloud_csrf",
        ));
      }
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
        "muxvia_cloud_csrf",
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
        "muxvia_cloud_csrf",
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
        "muxvia_cloud_csrf",
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

  async function revokeOverride(overrideId: string, revision: bigint) {
    const accountId = detail?.commerce?.account?.accountId;
    const reason = overrideRevokeReasons[overrideId]?.trim();
    if (!accountId || !reason) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/entitlement-overrides/revoke",
        RevokeEntitlementOverrideRequestSchema,
        create(RevokeEntitlementOverrideRequestSchema, { accountId, overrideId, expectedRevision: revision, reason, requestId: crypto.randomUUID() }),
        RevokeEntitlementOverrideResponseSchema,
        "muxvia_cloud_csrf",
      );
      setOverrideRevokeReasons((current) => ({ ...current, [overrideId]: "" }));
      await select(accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Entitlement override revoke failed");
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
        "muxvia_cloud_csrf",
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

  async function loadPromotionRedemptions(promotionId: string) {
    try {
      const page = create(PageRequestSchema, { pageSize: 100 });
      setPromotionRedemptions(await protoPost(
        "/api/v1/operator/promotions/redemptions",
        ListPromotionRedemptionsRequestSchema,
        create(ListPromotionRedemptionsRequestSchema, { promotionId, page }),
        ListPromotionRedemptionsResponseSchema,
        "muxvia_cloud_csrf",
      ));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Promotion redemptions failed");
    }
  }

  async function disablePromotion() {
    const promotion = promotions?.promotions.find((item) => item.promotionId === activeResourceId);
    if (!promotion || !promotionDisableReason.trim()) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/promotions/disable",
        DisablePromotionRequestSchema,
        create(DisablePromotionRequestSchema, { promotionId: promotion.promotionId, expectedRevision: promotion.revision, reason: promotionDisableReason.trim(), requestId: crypto.randomUUID() }),
        DisablePromotionResponseSchema,
        "muxvia_cloud_csrf",
      );
      setPromotionDisableReason("");
      await load("", "promotions", true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Promotion disable failed");
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
        "muxvia_cloud_csrf",
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

  async function reconcileCreemAttempt(paymentAttemptId: string) {
    const reason = reconcileReasons[paymentAttemptId]?.trim();
    if (!reason) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/orders/reconcile-creem",
        ReconcileCreemPaymentAttemptRequestSchema,
        create(ReconcileCreemPaymentAttemptRequestSchema, {
          paymentAttemptId,
          reason,
          requestId: crypto.randomUUID(),
        }),
        ReconcileCreemPaymentAttemptResponseSchema,
        "muxvia_cloud_csrf",
      );
      setReconcileReasons((current) => ({ ...current, [paymentAttemptId]: "" }));
      await load();
      if (detail?.commerce?.account?.accountId) await select(detail.commerce.account.accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Creem reconciliation failed");
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
        "muxvia_cloud_csrf",
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

  async function revokeDevice(deviceId: string, authEpoch: bigint, explicitAccountId?: string) {
    const accountId = explicitAccountId || detail?.commerce?.account?.accountId;
    if (!accountId) return;
    try {
      await protoPost(
        "/api/v1/operator/commands",
        CreateManagementCommandRequestSchema,
        create(CreateManagementCommandRequestSchema, {
          accountId,
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
        "muxvia_cloud_csrf",
      );
      if (detail?.commerce?.account?.accountId === accountId)
        await select(accountId);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Device revoke failed");
    }
  }

  async function migrateAssignment(daemonDeviceId: string, targetHubId: string, explicitAccountId?: string) {
    const accountId = explicitAccountId || detail?.commerce?.account?.accountId;
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
        "muxvia_cloud_csrf",
      );
      if (detail?.commerce?.account?.accountId === accountId)
        await select(accountId);
      await load();
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Assignment migration failed",
      );
    }
  }

  async function kickAgent(accountId: string, daemonDeviceId: string, assignmentEpoch: bigint, presenceSessionId: string) {
    if (!accountId || !daemonDeviceId || assignmentEpoch === 0n || !presenceSessionId) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/commands",
        CreateManagementCommandRequestSchema,
        create(CreateManagementCommandRequestSchema, {
          accountId,
          commandKind: ManagementCommandKind.KICK_PRESENCE,
          idempotencyKey: crypto.randomUUID(),
          target: create(ManagementCommandTargetSchema, { target: { case: "presence", value: create(KickPresenceTargetSchema, { daemonDeviceId, assignmentEpoch, presenceSessionId }) } }),
        }),
        CreateManagementCommandResponseSchema,
        "muxvia_cloud_csrf",
      );
      await load();
      if (detail?.commerce?.account?.accountId === accountId) await select(accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Agent kick failed");
    } finally {
      setBusy(false);
    }
  }

  async function revokeSession(sessionId: string, revision: bigint) {
    const accountId = detail?.commerce?.account?.accountId;
    if (!accountId || !window.confirm("Revoke this account session?")) return;
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/accounts/sessions/revoke",
        RevokeOperatorAccountSessionRequestSchema,
        create(RevokeOperatorAccountSessionRequestSchema, { accountId, sessionId, expectedRevision: revision, reason: "Operator revoked account session", requestId: crypto.randomUUID() }),
        RevokeOperatorAccountSessionResponseSchema,
        "muxvia_cloud_csrf",
      );
      await select(accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Session revoke failed");
    } finally {
      setBusy(false);
    }
  }

  async function publishRelease(event: FormEvent) {
    event.preventDefault();
    if (!releaseDraft.trim() || !releaseReason.trim()) return;
    setBusy(true);
    setError("");
    try {
      const artifact = fromJsonString(ReleaseArtifactProjectionSchema, releaseDraft, { ignoreUnknownFields: false });
      await protoPost("/api/v1/operator/releases/publish", PublishReleaseArtifactRequestSchema, create(PublishReleaseArtifactRequestSchema, { artifact, reason: releaseReason.trim(), requestId: crypto.randomUUID() }), PublishReleaseArtifactResponseSchema, "muxvia_cloud_csrf");
      setReleaseDraft("");
      setReleaseReason("");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Release publish failed");
    } finally {
      setBusy(false);
    }
  }

  async function setReleaseChannel(releaseId: string, paused: boolean, allowRollback: boolean) {
    const artifact = releases?.artifacts.find((item) => item.releaseId === releaseId);
    if (!artifact) return;
    const channel = releases?.channels.find((item) => item.product === artifact.product && item.channel === artifact.channel && item.os === artifact.os && item.arch === artifact.arch);
    setBusy(true);
    setError("");
    try {
      await protoPost("/api/v1/operator/releases/channel", SetReleaseChannelRequestSchema, create(SetReleaseChannelRequestSchema, { releaseId, expectedRevision: channel?.revision ?? 0n, paused, allowRollback, reason: paused ? "Operator paused release channel" : allowRollback ? "Operator rolled back release channel" : "Operator activated release", requestId: crypto.randomUUID() }), SetReleaseChannelResponseSchema, "muxvia_cloud_csrf");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Release channel update failed");
    } finally {
      setBusy(false);
    }
  }

  async function createHub(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await protoPost(
        "/api/v1/operator/fleet/create",
        CreateHubDeploymentRequestSchema,
        create(CreateHubDeploymentRequestSchema, {
          hubId: hubForm.hubId.trim(), edgeDeploymentId: hubForm.edgeDeploymentId.trim(), relayId: hubForm.relayId.trim(), region: hubForm.region.trim(), publicLabel: hubForm.publicLabel.trim(), publicHubUrl: hubForm.publicHubUrl.trim(), healthUrl: hubForm.healthUrl.trim(), maxAssignments: BigInt(hubForm.maxAssignments), hubControlPublicKey: decodeToken(hubForm.hubControlPublicKey.trim()), relayControlPublicKey: decodeToken(hubForm.relayControlPublicKey.trim()), reason: hubForm.reason.trim(), requestId: crypto.randomUUID(),
        }),
        CreateHubDeploymentResponseSchema,
        "muxvia_cloud_csrf",
      );
      setHubForm(emptyHubForm);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Hub creation failed");
    } finally {
      setBusy(false);
    }
  }

  function beginHubEdit(hub: NonNullable<ListHubFleetResponse["hubs"]>[number]) {
    const deployment = hub.deployment;
    if (!deployment?.metadata) return;
    setEditingHubId(deployment.metadata.hubId);
    setHubEdit({ region: deployment.metadata.region, publicLabel: deployment.metadata.publicLabel, publicHubUrl: deployment.publicHubUrl, healthUrl: deployment.healthUrl, maxAssignments: deployment.maxAssignments.toString(), reason: "" });
  }

  async function updateHub(event: FormEvent) {
    event.preventDefault();
    const deployment = fleet?.hubs.find((hub) => hub.deployment?.metadata?.hubId === editingHubId)?.deployment;
    if (!deployment) return;
    setBusy(true);
    try {
      await protoPost("/api/v1/operator/fleet/update", UpdateHubDeploymentRequestSchema, create(UpdateHubDeploymentRequestSchema, { hubId: editingHubId, expectedRevision: deployment.directoryRevision, region: hubEdit.region.trim(), publicLabel: hubEdit.publicLabel.trim(), publicHubUrl: hubEdit.publicHubUrl.trim(), healthUrl: hubEdit.healthUrl.trim(), maxAssignments: BigInt(hubEdit.maxAssignments), reason: hubEdit.reason.trim(), requestId: crypto.randomUUID() }), UpdateHubDeploymentResponseSchema, "muxvia_cloud_csrf");
      setEditingHubId("");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Hub update failed");
    } finally {
      setBusy(false);
    }
  }

  async function approveHub(hubId: string) {
    const deployment = fleet?.hubs.find((hub) => hub.deployment?.metadata?.hubId === hubId)?.deployment;
    if (!deployment?.metadata) return;
    await mutateHub("/api/v1/operator/fleet/approve", ApproveHubDeploymentIdentityRequestSchema, create(ApproveHubDeploymentIdentityRequestSchema, { hubId, expectedRevision: deployment.directoryRevision, hubControlIdentityFingerprint: deployment.metadata.hubControlIdentityFingerprint, relayControlIdentityFingerprint: deployment.metadata.relayControlIdentityFingerprint, reason: "Operator reviewed both Edge fingerprints", requestId: crypto.randomUUID() }), ApproveHubDeploymentIdentityResponseSchema);
  }

  async function setHubDrain(hubId: string, draining: boolean) {
    const deployment = fleet?.hubs.find((hub) => hub.deployment?.metadata?.hubId === hubId)?.deployment;
    if (!deployment) return;
    await mutateHub("/api/v1/operator/fleet/drain", SetHubDeploymentDrainRequestSchema, create(SetHubDeploymentDrainRequestSchema, { hubId, expectedRevision: deployment.directoryRevision, draining, reason: draining ? "Operator started maintenance drain" : "Operator cancelled maintenance drain", requestId: crypto.randomUUID() }), SetHubDeploymentDrainResponseSchema);
  }

  async function disableHub(hubId: string) {
    const deployment = fleet?.hubs.find((hub) => hub.deployment?.metadata?.hubId === hubId)?.deployment;
    if (!deployment || !window.confirm(`Disable and archive ${hubId}? This cannot be undone.`)) return;
    await mutateHub("/api/v1/operator/fleet/disable", DisableHubDeploymentRequestSchema, create(DisableHubDeploymentRequestSchema, { hubId, expectedRevision: deployment.directoryRevision, reason: "Operator completed drain and disabled deployment", requestId: crypto.randomUUID() }), DisableHubDeploymentResponseSchema);
  }

  async function mutateHub<Request extends DescMessage, Response extends DescMessage>(path: string, requestSchema: Request, request: MessageShape<Request>, responseSchema: Response) {
    setBusy(true);
    setError("");
    try {
      await protoPost(path, requestSchema, request, responseSchema, "muxvia_cloud_csrf");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Hub operation failed");
    } finally {
      setBusy(false);
    }
  }

  if (!workspaceLoaded)
    return (
      <main className="grid min-h-dvh place-items-center bg-background p-5 text-foreground">
        <p className="text-sm text-muted-foreground">{t("operator.loading")}</p>
      </main>
    );

  const allowedModules = operatorModules.filter((module) => workspaceModules.includes(module.permission));
  const activeResourceId = routePath.replace(/\/$/, "").split("/")[3] ? decodeURIComponent(routePath.replace(/\/$/, "").split("/")[3]) : "";
  const structuredCatalog = parseCatalog(catalogDraft);
  const selectedPromotion = promotions?.promotions.find((promotion) => promotion.promotionId === activeResourceId);

  return (
    <div className="min-h-dvh bg-background text-foreground lg:grid lg:grid-cols-[248px_minmax(0,1fr)]">
      <a className="sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:bg-primary focus:px-4 focus:py-3 focus:text-primary-foreground" href="#operator-content">{t("operator.navigation.skip")}</a>
      {navigationOpen && <button className="fixed inset-0 z-30 bg-foreground/20 lg:hidden" aria-label={t("operator.navigation.close")} onClick={() => setNavigationOpen(false)} />}
      <aside ref={navigationRef} id="operator-navigation" className={`${navigationOpen ? "translate-x-0" : "-translate-x-full"} fixed inset-y-0 left-0 z-40 flex w-[min(19rem,86vw)] flex-col border-r border-line bg-panel transition-transform motion-reduce:transition-none lg:sticky lg:top-0 lg:h-dvh lg:w-auto lg:translate-x-0 lg:transition-none`}>
        <div className="flex min-h-20 items-center justify-between gap-3 border-b border-line px-5">
          <a className="flex min-w-0 items-center gap-3" href="/operator">
            <b className="grid size-9 shrink-0 place-items-center bg-primary font-mono text-xs text-primary-foreground">MV</b>
            <span className="min-w-0"><strong className="block truncate text-sm">Muxvia Cloud</strong><small className="block truncate text-[10px] text-muted-foreground">{t("operator.navigation.workspace")}</small></span>
          </a>
          <Button className="lg:hidden" variant="ghost" size="icon" aria-label={t("operator.navigation.close")} onClick={() => setNavigationOpen(false)}><X /></Button>
        </div>
        <nav className="flex-1 overflow-y-auto p-3" aria-label={t("operator.navigation.label")}>
          {allowedModules.map(({ key, icon: Icon }) => (
            <a
              key={key}
              href={`/operator/${key}`}
              aria-current={activeModule === key ? "page" : undefined}
              className={`group relative flex min-h-12 items-center gap-3 border-b border-line px-3 text-sm focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary ${activeModule === key ? "bg-soft font-semibold text-primary" : "text-muted-foreground hover:bg-soft/70 hover:text-foreground"}`}
              onClick={(event) => { event.preventDefault(); navigate(key); }}
            >
              <span className={`absolute inset-y-2 left-0 w-0.5 ${activeModule === key ? "bg-primary" : "bg-transparent"}`} />
              <Icon className="size-4 shrink-0" />
              <span>{t(`operator.navigation.modules.${key}`)}</span>
            </a>
          ))}
        </nav>
        <div className="border-t border-line p-3">
          <a className="flex min-h-11 items-center gap-3 px-3 text-xs text-muted-foreground hover:bg-soft hover:text-foreground" href="/account"><UserRoundCog className="size-4" />{t("operator.account")}</a>
        </div>
      </aside>
      <main className="min-w-0 p-4 sm:p-6 lg:p-8 xl:p-10" id="operator-content" onClickCapture={protectMutation} onSubmitCapture={protectMutationSubmit}>
      <header className="flex flex-wrap items-center justify-between gap-4 border-b border-line pb-5">
        <div className="flex min-w-0 items-center gap-3">
          <Button ref={navigationTriggerRef} className="shrink-0 lg:hidden" variant="outline" size="icon" aria-label={t("operator.navigation.open")} aria-controls="operator-navigation" aria-expanded={navigationOpen} onClick={() => setNavigationOpen(true)}><Menu /></Button>
          <div className="min-w-0">
          <p className="font-mono text-[10px] text-primary">
            {t("operator.kicker")}
          </p>
          <h1 className="mt-2 truncate text-2xl font-semibold sm:text-3xl">{activeModule ? t(`operator.navigation.modules.${activeModule}`) : t("operator.title")}</h1>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <LanguageSwitcher compact />
          <Button aria-label={t("operator.refresh")} variant="outline" size="icon" onClick={() => void load()}>
            <RefreshCw />
          </Button>
        </div>
      </header>
      <div className="mt-4 flex min-h-11 items-center justify-between gap-3 border-b border-line text-xs">
        <span className="font-mono text-[10px] text-muted-foreground" aria-live="polite">{t(reauthExpiresAt > Date.now() ? "operator.reauth.unlocked" : "operator.reauth.readonly")}</span>
        {reauthExpiresAt <= Date.now() && <Button variant="ghost" onClick={(event) => setPendingMutation({ label: t("operator.reauth.genericAction"), replay: () => undefined, returnFocus: event.currentTarget })}>{t("operator.reauth.verify")}</Button>}
      </div>
      {pendingMutation && <div className="fixed inset-0 z-50 grid place-items-center bg-foreground/30 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setPendingMutation(undefined); }}>
        <section className="w-full max-w-md border border-line bg-panel p-5 shadow-xl" data-testid="operator-reauth-dialog" role="dialog" aria-modal="true" aria-labelledby="operator-reauth-title">
          <h2 className="text-lg font-semibold" id="operator-reauth-title">{t("operator.reauth.confirmTitle")}</h2>
          <p className="mt-2 text-sm text-muted-foreground">{t("operator.reauth.confirmCopy", { action: pendingMutation.label })}</p>
          <form className="mt-5 grid gap-4" onSubmit={reauthenticate} data-testid="operator-reauth">
            <label className="grid gap-2 text-xs font-medium">{t("operator.reauth.password")}<Input autoFocus type="password" autoComplete="current-password" value={reauthPassword} onChange={(event) => setReauthPassword(event.target.value)} /></label>
            <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={() => setPendingMutation(undefined)}>{t("operator.actions.cancel")}</Button><Button disabled={!reauthPassword} type="submit">{t("operator.reauth.unlock")}</Button></div>
          </form>
        </section>
      </div>}
      {error && (
        <p role="alert" className="mt-5 border border-destructive p-3 text-xs text-destructive">
          {error}
        </p>
      )}
      {!loaded && <div className="mt-6 border border-line bg-panel p-6 text-sm text-muted-foreground" role="status">{t("operator.loadingModule")}</div>}
      {activeModule === "releases" && <section className="mt-6 scroll-mt-4 border border-line bg-panel" data-testid="operator-releases" id="operator-releases">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-line p-4">
          <h2 className="flex items-center gap-2 text-sm font-medium"><Rocket className="size-4 text-primary" />{t("operator.releases.title")}</h2>
          <span className="font-mono text-[10px] text-muted-foreground">{t("operator.releases.count", { count: releases?.artifacts.length ?? 0 })}</span>
        </header>
        <div className="grid xl:grid-cols-[minmax(320px,0.8fr)_minmax(480px,1.2fr)]">
          <form className="grid gap-3 border-b border-line p-4 xl:border-b-0 xl:border-r" data-requires-recent-auth onSubmit={publishRelease}>
            <label className="grid gap-2 text-xs font-medium">{t("operator.releases.proto")}<textarea data-testid="release-draft" className="min-h-48 resize-y border border-line-strong bg-background p-3 font-mono text-xs outline-none focus:border-primary" value={releaseDraft} onChange={(event) => setReleaseDraft(event.target.value)} spellCheck={false} /></label>
            <label className="grid gap-2 text-xs font-medium">{t("operator.common.reason")}<Input data-testid="release-reason" value={releaseReason} onChange={(event) => setReleaseReason(event.target.value)} /></label>
            <Button data-requires-recent-auth data-testid="release-publish" disabled={busy || !releaseDraft.trim() || !releaseReason.trim()}>{t("operator.releases.publish")}</Button>
          </form>
          <div>
            {releases?.artifacts.length ? releases.artifacts.map((artifact) => {
              const channel = releases.channels.find((item) => item.product === artifact.product && item.channel === artifact.channel && item.os === artifact.os && item.arch === artifact.arch);
              const active = channel?.activeReleaseId === artifact.releaseId;
              return <div className="grid gap-3 border-b border-line p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-center" key={artifact.releaseId} data-testid={`release-${artifact.releaseId}`}>
                <span className="min-w-0"><a className="block truncate font-semibold hover:text-primary" href={`/operator/releases/${encodeURIComponent(artifact.releaseId)}`} onClick={(event) => { event.preventDefault(); navigateDetail("releases", artifact.releaseId); }}>{artifact.version} / {artifact.os}-{artifact.arch}</a><small className={`font-mono text-[10px] ${activeResourceId === artifact.releaseId ? "text-primary" : "text-muted-foreground"}`}>{artifact.releaseId} · {t("operator.releases.versionCode")} {artifact.versionCode.toString()} · {t("operator.releases.rollout")} {artifact.rolloutBasisPoints / 100}%</small><span className="mt-1 block text-xs text-muted-foreground">{t(active ? channel?.paused ? "operator.status.activePaused" : "operator.status.active" : "operator.status.historical")} · SHA-256 {Array.from(artifact.sha256.slice(0, 6)).map((value) => value.toString(16).padStart(2, "0")).join("")}…</span></span>
                <div className="flex flex-wrap gap-2">
                  {active ? <Button data-requires-recent-auth variant="outline" disabled={busy} onClick={() => void setReleaseChannel(artifact.releaseId, !channel?.paused, false)}>{t(channel?.paused ? "operator.actions.resume" : "operator.actions.pause")}</Button> : <><Button data-requires-recent-auth variant="outline" disabled={busy} onClick={() => void setReleaseChannel(artifact.releaseId, false, false)}>{t("operator.actions.activate")}</Button>{channel && artifact.versionCode < (releases.artifacts.find((item) => item.releaseId === channel.activeReleaseId)?.versionCode ?? 0n) && <Button data-requires-recent-auth variant="outline" disabled={busy} onClick={() => void setReleaseChannel(artifact.releaseId, false, true)}>{t("operator.actions.rollback")}</Button>}</>}
                </div>
              </div>;
            }) : <p className="p-4 text-xs text-muted-foreground">{t("operator.releases.empty")}</p>}
          </div>
        </div>
        {releases?.operatorAudit.length ? <div className="border-t border-line" data-testid="release-audit">
          {releases.operatorAudit.slice(0, 8).map((item) => <div className="grid gap-2 border-b border-line px-4 py-3 text-xs md:grid-cols-[180px_minmax(0,1fr)_auto]" key={item.auditId}>
            <span className="text-muted-foreground">{new Date(Number(item.occurredAtUnixMillis)).toLocaleString()}</span>
            <span><strong className="block">{item.action}</strong><small className="text-muted-foreground">{item.reason} / {item.actorId}</small></span>
            <span className="font-mono text-[10px] text-muted-foreground">REV {item.beforeRevision.toString()} → {item.afterRevision.toString()}</span>
          </div>)}
        </div> : null}
      </section>}
      {(activeModule === "orders" || activeModule === "subscriptions" || activeModule === "promotions") && <div className="mt-6 grid gap-5" data-testid="operator-commerce-operations">
        {activeModule === "orders" && <section className="scroll-mt-4 border border-line bg-panel" data-testid="operator-orders" id="operator-orders">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><ReceiptText className="size-4 text-primary" /><strong className="text-sm">{t("operator.orders.title")}</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{t("operator.common.total", { count: orders?.orders.length ?? 0 })}</span>
          </header>
          <form className="grid gap-3 border-b border-line p-4 sm:grid-cols-[1fr_1fr_1fr_auto]" onSubmit={(event) => { event.preventDefault(); void load("", "orders"); }}>
            <label className="grid gap-1 text-xs font-medium">{t("operator.filters.accountId")}<Input value={orderAccountId} onChange={(event) => setOrderAccountId(event.target.value)} /></label>
            <label className="grid gap-1 text-xs font-medium">{t("operator.filters.status")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={orderStatus} onChange={(event) => setOrderStatus(Number(event.target.value) as OrderStatus)}><option value={OrderStatus.UNSPECIFIED}>{t("operator.filters.all")}</option>{orderStatuses.map((status) => <option key={status} value={status}>{OrderStatus[status]}</option>)}</select></label>
            <label className="grid gap-1 text-xs font-medium">{t("operator.filters.provider")}<Input value={orderProvider} onChange={(event) => setOrderProvider(event.target.value)} /></label>
            <Button className="self-end" type="submit"><Search />{t("operator.actions.search")}</Button>
          </form>
          <div className="max-h-[36rem] overflow-y-auto">
            {orders?.orders.length ? orders.orders.map((item) => {
              const order = item.order;
              if (!order) return null;
              const manualSucceeded = item.paymentAttempts.some((attempt) => attempt.provider === "operator-manual" && attempt.status === PaymentAttemptStatus.SUCCEEDED);
              const canCollect = order.status === OrderStatus.PENDING && item.paymentAttempts.length === 0;
              const canReverse = order.status === OrderStatus.PAID && manualSucceeded;
              return (
                <div className="border-b border-line p-4" key={order.orderId} data-testid={`operator-order-${order.orderId}`}>
                  <div className="flex items-start justify-between gap-3">
                    <span className="min-w-0"><a className="block truncate text-sm font-semibold hover:text-primary" href={`/operator/orders/${encodeURIComponent(order.orderId)}`} onClick={(event) => { event.preventDefault(); navigateDetail("orders", order.orderId); }}>{order.planId} · {formatMoney(order.totalMinor, order.price?.currency, i18n.resolvedLanguage)}</a><small className={`font-mono text-[10px] ${activeResourceId === order.orderId ? "text-primary" : "text-muted-foreground"}`}>{order.orderId}</small></span>
                    <span className="text-[10px] font-semibold text-primary">{OrderStatus[order.status]}</span>
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">{t("operator.orders.summary", { account: order.accountId, attempts: item.paymentAttempts.length, events: item.paymentEvents.length })}</p>
                  {item.paymentAttempts.map((attempt) => {
                    const canReconcile = attempt.provider === "creem" && (attempt.status === PaymentAttemptStatus.PENDING || attempt.status === PaymentAttemptStatus.SUCCEEDED && Boolean(attempt.providerSubscriptionReference));
                    return <div className="mt-3 border-l-2 border-primary/40 pl-3 text-xs" key={attempt.paymentAttemptId} data-testid={`payment-attempt-${attempt.paymentAttemptId}`}>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <strong>{attempt.provider} · {PaymentAttemptStatus[attempt.status]}</strong>
                        <span className="font-mono text-[10px] text-muted-foreground">{t("operator.common.revision")} {attempt.revision.toString()}</span>
                      </div>
                      <dl className="mt-2 grid gap-1 text-muted-foreground">
                        {attempt.providerReference && <div><dt className="inline font-medium text-foreground">{t("operator.orders.checkout")} </dt><dd className="inline font-mono break-all">{attempt.providerReference}</dd></div>}
                        {attempt.providerTransactionReference && <div><dt className="inline font-medium text-foreground">{t("operator.orders.transaction")} </dt><dd className="inline font-mono break-all">{attempt.providerTransactionReference}</dd></div>}
                        {attempt.providerSubscriptionReference && <div><dt className="inline font-medium text-foreground">{t("operator.orders.subscription")} </dt><dd className="inline font-mono break-all">{attempt.providerSubscriptionReference}</dd></div>}
                        <div><dt className="inline font-medium text-foreground">{t("operator.orders.providerStatus")} </dt><dd className="inline">{attempt.lastProviderStatus || t("operator.orders.awaitingResponse")}</dd></div>
                        <div><dt className="inline font-medium text-foreground">{t("operator.orders.reconciliation")} </dt><dd className="inline">{t("operator.orders.checks", { count: attempt.reconcileAttempts })}{attempt.reconcileAfterUnixMillis > 0n ? ` · ${t("operator.orders.next", { time: new Date(Number(attempt.reconcileAfterUnixMillis)).toLocaleString() })}` : ` · ${t("operator.orders.stopped")}`}</dd></div>
                      </dl>
                      {canReconcile && <div className="mt-2 flex flex-col gap-2 sm:flex-row">
                        <Input aria-label={t("operator.orders.reconcileReasonFor", { id: attempt.paymentAttemptId })} placeholder={t("operator.orders.reconcileReason")} value={reconcileReasons[attempt.paymentAttemptId] ?? ""} onChange={(event) => setReconcileReasons((current) => ({ ...current, [attempt.paymentAttemptId]: event.target.value }))} />
                        <Button data-requires-recent-auth size="sm" variant="outline" disabled={busy || !reconcileReasons[attempt.paymentAttemptId]?.trim()} onClick={() => void reconcileCreemAttempt(attempt.paymentAttemptId)}>
                          <RefreshCw />
                          {t("operator.orders.reconcile")}
                        </Button>
                      </div>}
                    </div>;
                  })}
                  {(canCollect || canReverse) && <div className="mt-3 grid gap-2">
                    <Input aria-label={t("operator.orders.reasonFor", { id: order.orderId })} placeholder={t("operator.orders.operationReason")} value={paymentReasons[order.orderId] ?? ""} onChange={(event) => setPaymentReasons((current) => ({ ...current, [order.orderId]: event.target.value }))} />
                    <div className="flex flex-wrap gap-2">
                      {canCollect && <Button data-requires-recent-auth size="sm" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.SUCCEEDED)}>{t("operator.orders.recordPayment")}</Button>}
                      {canReverse && <Button data-requires-recent-auth size="sm" variant="outline" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.REFUNDED)}>{t("operator.actions.refund")}</Button>}
                      {canReverse && <Button data-requires-recent-auth size="sm" variant="outline" disabled={busy || !paymentReasons[order.orderId]?.trim()} onClick={() => void applyPaymentEvent(order.orderId, PaymentEventType.REVOKED)}>{t("operator.actions.revoke")}</Button>}
                    </div>
                  </div>}
                  {item.paymentEvents.length > 0 && <details className="mt-3 text-xs"><summary className="cursor-pointer text-muted-foreground">{t("operator.orders.timeline")}</summary><div className="mt-2 border-l border-line pl-3">{item.paymentEvents.slice().reverse().map((event) => <p className="py-1" key={event.event?.providerEventId}><strong>{PaymentEventType[event.event?.eventType ?? 0]}</strong> · {event.event?.provider} · {new Date(Number(event.event?.occurredAtUnixMillis ?? 0n)).toLocaleString()}</p>)}</div></details>}
                </div>
              );
            }) : <p className="p-4 text-xs text-muted-foreground">{t("operator.orders.empty")}</p>}
          </div>
        </section>}
        {activeModule === "subscriptions" && <section className="scroll-mt-4 border border-line bg-panel" data-testid="operator-subscriptions" id="operator-subscriptions">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><UserRoundCog className="size-4 text-primary" /><strong className="text-sm">{t("operator.subscriptions.title")}</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{t("operator.common.total", { count: subscriptions?.subscriptions.length ?? 0 })}</span>
          </header>
          <form className="flex flex-col gap-3 border-b border-line p-4 sm:flex-row sm:items-end" onSubmit={(event) => { event.preventDefault(); void load("", "subscriptions"); }}>
            <label className="grid flex-1 gap-1 text-xs font-medium">{t("operator.filters.status")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={subscriptionStatus} onChange={(event) => setSubscriptionStatus(Number(event.target.value) as SubscriptionStatus)}><option value={SubscriptionStatus.UNSPECIFIED}>{t("operator.filters.all")}</option>{subscriptionStatuses.map((status) => <option key={status} value={status}>{SubscriptionStatus[status]}</option>)}</select></label>
            <Button type="submit"><Search />{t("operator.actions.search")}</Button>
          </form>
          <div className="grid lg:grid-cols-[minmax(280px,0.8fr)_minmax(420px,1.2fr)]">
          <div className="max-h-[42rem] overflow-y-auto border-b border-line lg:border-b-0 lg:border-r">
            {subscriptions?.subscriptions.map((subscription) => <button type="button" className="grid min-h-16 w-full grid-cols-[1fr_auto] items-center gap-3 border-b border-line p-4 text-left hover:bg-soft focus-visible:outline-2 focus-visible:outline-primary" key={subscription.subscriptionId} onClick={() => navigateDetail("subscriptions", subscription.subscriptionId, subscription.accountId)}>
              <span><strong className="block text-sm">{subscription.planId} v{subscription.planVersion.toString()}</strong><small className="font-mono text-[10px] text-muted-foreground">{subscription.accountId}</small></span>
              <span className="text-[10px] font-semibold text-primary">{SubscriptionStatus[subscription.status]}</span>
            </button>)}
          </div>
          {detail ? <div className="min-w-0">
            <header className="border-b border-line p-5">
              <p className="font-mono text-[10px] text-muted-foreground">{detail.commerce?.account?.email}</p>
              <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
                <div><h3 className="text-lg font-medium">{detail.commerce?.subscription?.planId}</h3><p className="text-xs text-muted-foreground">{SubscriptionStatus[detail.commerce?.subscription?.status ?? 0]}</p></div>
                <div className="flex gap-2">
                  <Button data-requires-recent-auth variant="outline" data-testid="operator-suspend" onClick={() => void transition(SubscriptionTransitionKind.SUSPEND)}>{t("operator.actions.suspend")}</Button>
                  <Button data-requires-recent-auth variant="outline" data-testid="operator-restore" onClick={() => void transition(SubscriptionTransitionKind.RESTORE)}>{t("operator.actions.restore")}</Button>
                </div>
              </div>
            </header>
            <div data-testid="operator-subscription-adjustment">
              <header className="flex items-center gap-2 p-4"><UserRoundCog className="size-4 text-primary" /><div><h3 className="text-sm font-medium">{t("operator.adjustment.title")}</h3><p className="mt-1 text-xs text-muted-foreground">{t("operator.adjustment.copy")}</p></div></header>
              <form className="grid gap-3 border-t border-line p-4 md:grid-cols-2" data-requires-recent-auth onSubmit={adjustSubscription}>
                <label className="grid gap-2 text-xs font-medium">{t("operator.adjustment.kind")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={adjustmentKind} onChange={(event) => setAdjustmentKind(Number(event.target.value) as SubscriptionAdjustmentKind)}><option value={SubscriptionAdjustmentKind.GRANT}>{t("operator.adjustment.grant")}</option><option value={SubscriptionAdjustmentKind.EXTEND}>{t("operator.adjustment.extend")}</option><option value={SubscriptionAdjustmentKind.CHANGE_PLAN}>{t("operator.adjustment.changePlan")}</option></select></label>
                <label className="grid gap-2 text-xs font-medium">{t("operator.adjustment.days")}<Input type="number" min="1" value={adjustmentDays} onChange={(event) => setAdjustmentDays(event.target.value)} /></label>
                {adjustmentKind !== SubscriptionAdjustmentKind.EXTEND && <label className="grid gap-2 text-xs font-medium">{t("operator.adjustment.targetPlan")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={adjustmentPlan} onChange={(event) => setAdjustmentPlan(event.target.value)}>{catalogHistory?.releases.find((item) => item.active)?.catalog?.plans.filter((plan) => !plan.included).map((plan) => <option value={plan.planId} key={plan.planId}>{plan.presentation?.name || plan.planId}</option>)}</select></label>}
                <label className="grid gap-2 text-xs font-medium">{t("operator.common.reason")}<Input data-testid="adjustment-reason" value={adjustmentReason} onChange={(event) => setAdjustmentReason(event.target.value)} /></label>
                <Button data-requires-recent-auth data-testid="adjustment-create" className="md:col-start-2" disabled={busy || !adjustmentReason.trim() || Number(adjustmentDays) < 1}>{t("operator.adjustment.apply")}</Button>
              </form>
              <div className="border-t border-line">{detail.commerce?.subscriptionAdjustments.length ? detail.commerce.subscriptionAdjustments.map((item) => <div className="grid grid-cols-[1fr_auto] gap-3 border-b border-line px-4 py-3 text-xs" key={item.adjustmentId}><span><strong className="block">{SubscriptionAdjustmentKind[item.adjustmentKind]} · {t("operator.adjustment.dayCount", { count: item.durationDays })}</strong><small className="text-muted-foreground">{item.reason} · {item.actorId}</small></span><span className="font-mono text-[10px] text-muted-foreground">REV {item.resultingSubscriptionRevision.toString()}</span></div>) : <p className="p-4 text-xs text-muted-foreground">{t("operator.adjustment.empty")}</p>}</div>
            </div>
          </div> : <p className="p-8 text-sm text-muted-foreground">{t("operator.subscriptions.select")}</p>}
          </div>
        </section>}
        {activeModule === "promotions" && <section className="scroll-mt-4 border border-line bg-panel" data-testid="operator-promotions" id="operator-promotions">
          <header className="flex items-center justify-between gap-3 border-b border-line p-4">
            <span className="flex items-center gap-2"><TicketPercent className="size-4 text-primary" /><strong className="text-sm">{t("operator.promotions.title")}</strong></span>
            <span className="font-mono text-[10px] text-muted-foreground">{t("operator.common.releases", { count: promotions?.promotions.length ?? 0 })}</span>
          </header>
          <form className="grid gap-3 border-b border-line p-4" data-requires-recent-auth onSubmit={createPromotion}>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="grid gap-2 text-xs font-medium">{t("operator.promotions.code")}<Input data-testid="promotion-code" value={promotionCode} onChange={(event) => setPromotionCode(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">{t("operator.promotions.plan")}<Input value={promotionPlan} onChange={(event) => setPromotionPlan(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">{t("operator.promotions.discount")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={promotionKind} onChange={(event) => setPromotionKind(Number(event.target.value) as PromotionDiscountKind)}><option value={PromotionDiscountKind.PERCENT}>{t("operator.promotions.percent")}</option><option value={PromotionDiscountKind.FIXED}>{t("operator.promotions.fixed")}</option></select></label>
              <label className="grid gap-2 text-xs font-medium">{t("operator.common.value")}<Input type="number" min="1" value={promotionPercent} onChange={(event) => setPromotionPercent(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">{t("operator.promotions.limit")}<Input type="number" min="1" value={promotionLimit} onChange={(event) => setPromotionLimit(event.target.value)} /></label>
              <label className="grid gap-2 text-xs font-medium">{t("operator.common.effectiveUntil")}<Input type="datetime-local" value={promotionUntil} onChange={(event) => setPromotionUntil(event.target.value)} /></label>
            </div>
            <label className="grid gap-2 text-xs font-medium">{t("operator.promotions.creemCode")}<Input value={promotionCreemCode} onChange={(event) => setPromotionCreemCode(event.target.value)} /></label>
            <label className="grid gap-2 text-xs font-medium">{t("operator.common.publishReason")}<Input value={promotionReason} onChange={(event) => setPromotionReason(event.target.value)} /></label>
            <Button data-requires-recent-auth data-testid="promotion-create" disabled={busy || !promotionCode.trim() || !promotionCreemCode.trim() || !promotionReason.trim() || !promotionUntil}>{t("operator.promotions.publish")}</Button>
          </form>
          <div className="grid lg:grid-cols-[minmax(280px,0.8fr)_minmax(420px,1.2fr)]">
            <div className="max-h-[36rem] overflow-y-auto border-b border-line lg:border-b-0 lg:border-r">{promotions?.promotions.map((promotion) => <div className={`grid grid-cols-[1fr_auto] gap-3 border-b p-4 text-xs ${activeResourceId === promotion.promotionId ? "border-primary bg-soft" : "border-line"}`} key={promotion.promotionId}><span><a className="block text-sm font-semibold hover:text-primary" href={`/operator/promotions/${encodeURIComponent(promotion.promotionId)}`} onClick={(event) => { event.preventDefault(); navigateDetail("promotions", promotion.promotionId); }}>{promotion.code}</a><small className="text-muted-foreground">{promotion.planIds.join(", ")} · revision {promotion.revision.toString()}</small></span><span className={promotion.state === PromotionState.ACTIVE ? "text-success" : "text-muted-foreground"}>{PromotionState[promotion.state]}</span></div>)}</div>
            {selectedPromotion ? <div className="min-w-0 p-4" data-testid="promotion-detail">
              <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-lg font-semibold">{selectedPromotion.code}</h3><p className="font-mono text-[10px] text-muted-foreground">{selectedPromotion.promotionId}</p></div><span className={selectedPromotion.state === PromotionState.ACTIVE ? "text-success" : "text-muted-foreground"}>{PromotionState[selectedPromotion.state]}</span></div>
              <dl className="mt-4 grid gap-3 text-xs sm:grid-cols-2">
                <div><dt className="text-muted-foreground">{t("operator.promotions.discount")}</dt><dd className="mt-1 font-medium">{selectedPromotion.discountKind === PromotionDiscountKind.PERCENT ? `${selectedPromotion.percentBasisPoints / 100}%` : formatMoney(selectedPromotion.fixedMinor, selectedPromotion.currency, i18n.resolvedLanguage)}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.promotions.plan")}</dt><dd className="mt-1 font-medium">{selectedPromotion.planIds.join(", ")}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.promotions.limit")}</dt><dd className="mt-1 font-medium">{selectedPromotion.maxRedemptions}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.promotions.creemCode")}</dt><dd className="mt-1 break-all font-mono">{selectedPromotion.creemDiscountCode}</dd></div>
              </dl>
              {selectedPromotion.state === PromotionState.ACTIVE && <div className="mt-4 flex flex-col gap-2 sm:flex-row"><Input aria-label={t("operator.promotions.disableReason")} placeholder={t("operator.promotions.disableReason")} value={promotionDisableReason} onChange={(event) => setPromotionDisableReason(event.target.value)} /><Button data-requires-recent-auth variant="outline" disabled={busy || !promotionDisableReason.trim()} onClick={() => void disablePromotion()}>{t("operator.promotions.disable")}</Button></div>}
              <div className="mt-5 border-t border-line"><h4 className="py-3 text-sm font-medium">{t("operator.promotions.redemptions")}</h4>{promotionRedemptions?.redemptions.length ? promotionRedemptions.redemptions.map((redemption) => <div className="grid gap-1 border-t border-line py-3 text-xs" key={redemption.redemptionId}><strong>{redemption.accountId} · {PromotionRedemptionState[redemption.state]}</strong><span className="text-muted-foreground">{redemption.orderId} · {formatMoney(redemption.discountMinor, selectedPromotion.currency || "USD", i18n.resolvedLanguage)}</span></div>) : <p className="border-t border-line py-3 text-xs text-muted-foreground">{t("operator.promotions.noRedemptions")}</p>}</div>
            </div> : <p className="p-8 text-sm text-muted-foreground">{t("operator.promotions.select")}</p>}
          </div>
        </section>}
      </div>}
      {activeModule === "plans" && <section className="mt-6 scroll-mt-4 border border-line bg-panel" data-testid="operator-catalog" id="operator-catalog">
        <header className="flex flex-wrap items-center justify-between gap-4 border-b border-line p-4">
          <div className="flex items-center gap-3">
            <PackageOpen className="size-4 text-primary" />
            <div>
              <h2 className="text-sm font-medium">{t("operator.catalog.title")}</h2>
              <p className="mt-1 text-xs text-muted-foreground">{t("operator.catalog.copy")}</p>
            </div>
          </div>
          <span className="font-mono text-[10px] text-muted-foreground">
            {t("operator.common.releases", { count: catalogHistory?.releases.length ?? 0 })}
          </span>
        </header>
        <div className="grid lg:grid-cols-[minmax(260px,0.7fr)_minmax(420px,1.3fr)]">
          <div className="border-b border-line lg:border-b-0 lg:border-r">
            {catalogHistory?.releases.map((release) => (
              <button
                type="button"
                key={release.catalog?.catalogVersion.toString()}
                className="grid min-h-16 w-full grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line px-4 py-3 text-left hover:bg-soft focus-visible:outline-2 focus-visible:outline-primary"
                onClick={() => { if (release.catalog) { showCatalog(release.catalog); navigateDetail("plans", release.catalog.catalogVersion.toString()); } }}
              >
                <History className="size-4 text-muted-foreground" />
                <span>
                  <strong className="block text-sm">{t("operator.catalog.version", { version: release.catalog?.catalogVersion.toString() })}</strong>
                  <small className="text-muted-foreground">{new Date(Number(release.publishedAtUnixMillis)).toLocaleString()}</small>
                </span>
                <span className={release.active ? "text-[10px] font-semibold text-success" : "text-[10px] text-muted-foreground"}>
                  {t(release.active ? "operator.status.active" : "operator.status.history")}
                </span>
              </button>
            ))}
          </div>
          <form className="grid gap-4 p-4" data-requires-recent-auth onSubmit={publishCatalog}>
            {structuredCatalog && <div className="grid gap-4" data-testid="catalog-structured-editor">
              <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.catalogVersion")}<Input type="number" min="1" value={structuredCatalog.catalogVersion.toString()} onChange={(event) => updateCatalog((catalog) => { catalog.catalogVersion = BigInt(event.target.value || "0"); })} /></label>
              {structuredCatalog.plans.map((plan) => <fieldset className="grid gap-3 border border-line p-4" key={plan.planId}>
                <legend className="px-2 text-xs font-semibold">{plan.planId} · v{plan.planVersion.toString()}</legend>
                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.planName")}<Input value={plan.presentation?.name ?? ""} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.presentation) draft.presentation.name = event.target.value; })} /></label>
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.currency")}<Input value={plan.price?.currency ?? ""} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.price) draft.price.currency = event.target.value.toUpperCase(); })} /></label>
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.monthlyMinor")}<Input type="number" min="0" value={plan.price?.monthlyMinor.toString() ?? "0"} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.price) draft.price.monthlyMinor = BigInt(event.target.value || "0"); })} /></label>
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.yearlyMinor")}<Input type="number" min="0" value={plan.price?.yearlyMinor.toString() ?? "0"} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.price) draft.price.yearlyMinor = BigInt(event.target.value || "0"); })} /></label>
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.deviceLimit")}<Input type="number" min="0" value={plan.capability?.cloudDeviceLimit ?? 0} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.capability) draft.capability.cloudDeviceLimit = Number(event.target.value); })} /></label>
                  <label className="grid gap-2 text-xs font-medium">{t("operator.catalog.p2pConcurrency")}<Input type="number" min="0" value={plan.capability?.managedP2pMaxConcurrency ?? 0} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.capability) draft.capability.managedP2pMaxConcurrency = Number(event.target.value); })} /></label>
                  <label className="grid gap-2 text-xs font-medium sm:col-span-2">{t("operator.catalog.relayPeriodBytes")}<Input type="number" min="0" value={plan.capability?.relay?.maxBytesPerPeriod.toString() ?? "0"} onChange={(event) => updateCatalogPlan(plan.planId, (draft) => { if (draft.capability?.relay) draft.capability.relay.maxBytesPerPeriod = BigInt(event.target.value || "0"); })} /></label>
                </div>
              </fieldset>)}
            </div>}
            <details className="border border-line" data-testid="catalog-advanced-editor">
              <summary className="min-h-11 cursor-pointer px-4 py-3 text-xs font-medium">{t("operator.catalog.advanced")}</summary>
              <label className="grid gap-2 border-t border-line p-4 text-xs font-medium">
                {t("operator.catalog.proto")}
                <textarea
                  ref={catalogEditorRef}
                  data-testid="catalog-editor"
                  className="min-h-64 w-full resize-y border border-line-strong bg-background p-3 font-mono text-xs leading-5 outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
                  value={catalogDraft}
                  onChange={(event) => setCatalogDraft(event.target.value)}
                  spellCheck={false}
                />
              </label>
            </details>
            <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
              <label className="grid gap-2 text-xs font-medium">
                {t("operator.common.reason")}
                <Input data-testid="catalog-reason" value={catalogReason} onChange={(event) => setCatalogReason(event.target.value)} placeholder={t("operator.catalog.reasonPlaceholder")} />
              </label>
              <Button data-requires-recent-auth data-testid="catalog-publish" disabled={busy || !catalogDraft || !catalogReason.trim()}>
                {t("operator.catalog.publish")}
              </Button>
            </div>
          </form>
        </div>
      </section>}
      {(activeModule === "users" || activeModule === "agents" || activeModule === "privileges") && <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(360px,0.8fr)_minmax(480px,1.2fr)]">
        <section className="min-w-0 scroll-mt-4 border border-line bg-panel" id="operator-directory">
          <header className="grid gap-3 border-b border-line p-4">
            <h2 className="flex min-h-10 items-center gap-2 text-sm font-medium">
              {activeModule === "agents" ? <Laptop className="size-4 text-primary" /> : activeModule === "privileges" ? <SlidersHorizontal className="size-4 text-primary" /> : <UserRoundCog className="size-4 text-primary" />}
              {activeModule ? t(`operator.navigation.modules.${activeModule}`) : ""}
            </h2>
            <div className="flex items-center gap-2">
              <Search className="size-4 shrink-0" />
              <Input
                className="min-w-0 flex-1"
                value={query}
                placeholder={t(directoryView === "users" ? "operator.directory.searchUsers" : "operator.directory.searchAgents")}
                onChange={(event) => setQuery(event.target.value)}
              />
              <Button onClick={() => void load(query)}>{t("operator.actions.search")}</Button>
            </div>
            {activeModule === "users" && <label className="grid gap-1 text-xs font-medium">{t("operator.filters.subscriptionStatus")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={accountSubscriptionStatus} onChange={(event) => setAccountSubscriptionStatus(Number(event.target.value) as SubscriptionStatus)}><option value={SubscriptionStatus.UNSPECIFIED}>{t("operator.filters.all")}</option>{subscriptionStatuses.map((status) => <option key={status} value={status}>{SubscriptionStatus[status]}</option>)}</select></label>}
            {activeModule === "agents" && <label className="grid gap-1 text-xs font-medium">{t("operator.filters.freshness")}<select className="min-h-11 border border-line-strong bg-background px-3 text-sm" value={agentFreshness} onChange={(event) => setAgentFreshness(Number(event.target.value) as Freshness)}><option value={Freshness.UNSPECIFIED}>{t("operator.filters.all")}</option><option value={Freshness.FRESH}>{Freshness[Freshness.FRESH]}</option><option value={Freshness.STALE}>{Freshness[Freshness.STALE]}</option></select></label>}
          </header>
          {directoryView === "users" && accounts?.accounts.map((item) => (
            <button
              className="grid w-full gap-2 border-b border-line p-4 text-left hover:bg-soft"
              key={item.account?.accountId}
              data-testid={`operator-account-${item.account?.accountId}`}
              onClick={() => item.account && navigateDetail(activeModule === "privileges" ? "privileges" : "users", item.account.accountId, item.account.accountId)}
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
          {directoryView === "agents" && agents?.agents.map((agent) => {
            const accountId = agent.account?.accountId ?? "";
            const device = agent.device;
            const presence = agent.presence;
            const isFreshOnline = presence?.availability === Availability.ONLINE && presence.freshness === Freshness.FRESH;
            const state = presence?.freshness === Freshness.STALE
              ? "STALE"
              : Availability[presence?.availability ?? Availability.UNSPECIFIED];
            const currentHubId = device?.assignedHubId || presence?.controlOwnerHubId;
            return (
              <div
                className="grid gap-3 border-b border-line p-4"
                key={`${accountId}/${device?.deviceId}`}
                data-testid={`operator-agent-${device?.deviceId}`}
              >
                <button className="min-w-0 text-left" onClick={() => navigateDetail("agents", device?.deviceId ?? accountId, accountId)}>
                  <span className="flex min-w-0 items-center gap-2 text-sm font-medium">
                    <Radio className={isFreshOnline ? "size-4 shrink-0 text-success" : "size-4 shrink-0 text-muted-foreground"} />
                    <span className="truncate">{device?.displayName || device?.deviceId}</span>
                    <span className={isFreshOnline ? "ml-auto text-[10px] text-success" : "ml-auto text-[10px] text-muted-foreground"}>{state}</span>
                  </span>
                  <span className="mt-1 block truncate text-xs text-muted-foreground">{agent.account?.email}</span>
                  <span className="mt-1 block font-mono text-[10px] text-muted-foreground">
                    {currentHubId || t("operator.directory.noAssignment")} / {t("operator.directory.activePeers", { count: agent.activePeerSessionCount.toString() })}
                  </span>
                </button>
                <div className="flex flex-wrap gap-2">
                  {isFreshOnline && presence && (
                    <Button
                      data-requires-recent-auth
                      variant="outline"
                      disabled={busy}
                      data-testid={`kick-${device?.deviceId}`}
                      onClick={() => void kickAgent(accountId, presence.daemonDeviceId, presence.assignmentEpoch, presence.presenceSessionId)}
                    >
                      {t("operator.actions.kick")}
                    </Button>
                  )}
                  {fleet?.hubs.filter((hub) => hub.hubReady && hub.deployment?.metadata?.hubId && hub.deployment.metadata.hubId !== currentHubId).map((hub) => (
                    <Button
                      data-requires-recent-auth
                      key={hub.deployment?.metadata?.hubId}
                      variant="outline"
                      disabled={busy || !device}
                      onClick={() => device && void migrateAssignment(device.deviceId, hub.deployment?.metadata?.hubId ?? "", accountId)}
                    >
                      {t("operator.actions.moveTo", { target: hub.deployment?.metadata?.publicLabel || hub.deployment?.metadata?.hubId })}
                    </Button>
                  ))}
                  <Button
                    data-requires-recent-auth
                    variant="outline"
                    disabled={busy || !device || device.revoked}
                    data-testid={`agent-revoke-${device?.deviceId}`}
                    onClick={() => device && void revokeDevice(device.deviceId, device.authEpoch, accountId)}
                  >
                    {t("operator.actions.revoke")}
                  </Button>
                </div>
              </div>
            );
          })}
        </section>
        <section className="min-w-0 border border-line bg-panel">
          {detail ? (
            <>
              <header className="border-b border-line p-5">
                <p className="font-mono text-[9px] text-muted-foreground">
                  {t("operator.detail.account")}
                </p>
                <h2 className="mt-2 text-xl">
                  {detail.commerce?.account?.email}
                </h2>
                <p className="mt-1 font-mono text-[10px] text-muted-foreground">
                  {detail.commerce?.account?.accountId}
                </p>
              </header>
              <div className="grid gap-4 p-5 md:grid-cols-2">
                <Stat
                  label={t("operator.detail.plan")}
                  value={detail.commerce?.subscription?.planId ?? "-"}
                />
                <Stat
                  label={t("operator.detail.subscription")}
                  value={
                    SubscriptionStatus[
                      detail.commerce?.subscription?.status ?? 0
                    ]
                  }
                />
                <Stat
                  label={t("operator.detail.devices")}
                  value={String(detail.devices?.devices.length ?? 0)}
                />
                <Stat
                  label={t("operator.detail.peerSessions")}
                  value={String(detail.topology?.peerSessions.length ?? 0)}
                />
                <Stat
                  label={t("operator.detail.accountSessions")}
                  value={String(detail.sessions.filter((session) => !session.revoked).length)}
                />
              </div>
              {activeModule === "privileges" && <div className="border-t border-line" data-testid="operator-overrides">
                <header className="flex items-center gap-2 p-4">
                  <SlidersHorizontal className="size-4 text-primary" />
                  <div>
                    <h3 className="text-sm font-medium">{t("operator.privileges.title")}</h3>
                    <p className="mt-1 text-xs text-muted-foreground">{t("operator.privileges.copy")}</p>
                  </div>
                </header>
                <form className="grid gap-3 border-t border-line p-4 md:grid-cols-2" data-requires-recent-auth onSubmit={putOverride}>
                  <label className="grid gap-2 text-xs font-medium">
                    {t("operator.privileges.capability")}
                    <select className="min-h-11 border border-line-strong bg-background px-3 text-sm outline-none focus:border-primary" value={overridePath} onChange={(event) => setOverridePath(event.target.value)}>
                      <option value="cloud_device_limit">{t("operator.privileges.deviceLimit")}</option>
                      <option value="managed_p2p_max_concurrency">{t("operator.privileges.p2pConcurrency")}</option>
                    </select>
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    {t("operator.common.value")}
                    <Input data-testid="override-value" type="number" min="0" value={overrideValue} onChange={(event) => setOverrideValue(event.target.value)} />
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    {t("operator.common.effectiveUntil")}
                    <Input data-testid="override-until" type="datetime-local" value={overrideUntil} onChange={(event) => setOverrideUntil(event.target.value)} />
                  </label>
                  <label className="grid gap-2 text-xs font-medium">
                    {t("operator.common.reason")}
                    <Input data-testid="override-reason" value={overrideReason} onChange={(event) => setOverrideReason(event.target.value)} placeholder={t("operator.privileges.reasonPlaceholder")} />
                  </label>
                  <Button data-requires-recent-auth className="md:col-start-2" data-testid="override-create" disabled={busy || !overrideValue || !overrideUntil || !overrideReason.trim()}>
                    {t("operator.privileges.apply")}
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
                        {t(item.revokedAtUnixMillis > 0n ? "operator.status.revoked" : item.effectiveUntilUnixMillis <= BigInt(Date.now()) ? "operator.status.expired" : "operator.status.active")}
                      </span>
                      {item.revokedAtUnixMillis === 0n && item.effectiveUntilUnixMillis > BigInt(Date.now()) && <div className="flex flex-col gap-2 md:col-span-2 sm:flex-row"><Input aria-label={t("operator.privileges.revokeReasonFor", { id: item.overrideId })} placeholder={t("operator.privileges.revokeReason")} value={overrideRevokeReasons[item.overrideId] ?? ""} onChange={(event) => setOverrideRevokeReasons((current) => ({ ...current, [item.overrideId]: event.target.value }))} /><Button data-requires-recent-auth variant="outline" disabled={busy || !overrideRevokeReasons[item.overrideId]?.trim()} onClick={() => void revokeOverride(item.overrideId, item.revision)}>{t("operator.privileges.revoke")}</Button></div>}
                    </div>
                  )) : <p className="p-4 text-xs text-muted-foreground">{t("operator.privileges.empty")}</p>}
                </div>
              </div>}
              {activeModule === "users" && <div className="border-t border-line" data-testid="operator-account-sessions">
                <h3 className="p-4 text-sm font-medium">{t("operator.sessions.title")}</h3>
                {detail.sessions.length ? detail.sessions.map((session) => (
                  <div className="grid gap-3 border-t border-line px-4 py-3 text-xs md:grid-cols-[minmax(0,1fr)_auto] md:items-center" key={session.sessionId} data-testid={`account-session-${session.sessionId}`}>
                    <span className="min-w-0">
                      <strong className="block truncate">{session.clientDeviceId || t("operator.sessions.browser")}</strong>
                      <small className="font-mono text-muted-foreground">{t("operator.sessions.expires", { time: new Date(Number(session.refreshExpiresAtUnixMillis)).toLocaleString(), revision: session.revision.toString() })}</small>
                    </span>
                    {session.revoked ? (
                      <span className="text-muted-foreground">{t("operator.status.revoked")}</span>
                    ) : (
                      <Button data-requires-recent-auth variant="outline" disabled={busy} data-testid={`session-revoke-${session.sessionId}`} onClick={() => void revokeSession(session.sessionId, session.revision)}>{t("operator.sessions.revoke")}</Button>
                    )}
                  </div>
                )) : <p className="border-t border-line p-4 text-xs text-muted-foreground">{t("operator.sessions.empty")}</p>}
              </div>}
              {(activeModule === "users" || activeModule === "agents") && <div className="border-t border-line">
                <h3 className="p-4 text-sm font-medium">{t("operator.detail.devices")}</h3>
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
                                hub.deployment?.metadata?.hubId &&
                                hub.deployment.metadata.hubId !== currentHubId,
                            )
                            .map((hub) => (
                              <Button
                                data-requires-recent-auth
                                key={hub.deployment?.metadata?.hubId}
                                variant="outline"
                                data-testid={`migrate-${device.deviceId}-${hub.deployment?.metadata?.hubId}`}
                                onClick={() =>
                                  void migrateAssignment(
                                    device.deviceId,
                                    hub.deployment?.metadata?.hubId ?? "",
                                  )
                                }
                              >
                                {t("operator.actions.moveTo", { target: hub.deployment?.metadata?.publicLabel || hub.deployment?.metadata?.hubId })}
                              </Button>
                            ))}
                        <Button
                          data-requires-recent-auth
                          variant="outline"
                          disabled={device.revoked}
                          data-testid={`revoke-${device.deviceId}`}
                          onClick={() =>
                            void revokeDevice(device.deviceId, device.authEpoch)
                          }
                        >
                          {t("operator.actions.revoke")}
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>}
              {(activeModule === "users" || activeModule === "agents") && <div className="border-t border-line" data-testid="operator-command-results">
                <h3 className="p-4 text-sm font-medium">{t("operator.commands.title")}</h3>
                {detail.commands.length ? detail.commands.slice().reverse().map((command) => (
                  <div className="grid gap-2 border-t border-line px-4 py-3 text-xs" key={command.commandId} data-testid={`operator-command-${command.commandId}`}>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <strong>{ManagementCommandKind[command.commandKind]}</strong>
                      <span className="font-mono text-[10px] text-muted-foreground">{command.commandId}</span>
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-[10px] text-muted-foreground sm:grid-cols-4">
                      <span>AUTH {CommandAuthorityResult[command.authorityResult]}</span>
                      <span>DELIVERY {CommandDeliveryState[command.deliveryState]}</span>
                      <span>EXECUTION {CommandExecutionState[command.executionState]}</span>
                      <span>EFFECT {CommandObservedEffect[command.observedEffect]}</span>
                    </div>
                  </div>
                )) : <p className="border-t border-line p-4 text-xs text-muted-foreground">{t("operator.commands.empty")}</p>}
              </div>}
              <div className="border-t border-line" data-testid="operator-audit">
                <h3 className="p-4 text-sm font-medium">{t("operator.audit.title")}</h3>
                {detail.operatorAudit.length ? detail.operatorAudit.map((item) => (
                  <div className="grid gap-2 border-t border-line px-4 py-3 text-xs md:grid-cols-[180px_minmax(0,1fr)_auto]" key={item.auditId} data-testid={`operator-audit-${item.auditId}`}>
                    <span className="text-muted-foreground">{new Date(Number(item.occurredAtUnixMillis)).toLocaleString()}</span>
                    <span className="min-w-0"><strong className="block">{item.action}</strong><small className="text-muted-foreground">{item.reason} / {item.actorId}</small></span>
                    <span className="font-mono text-[10px] text-muted-foreground">REV {item.beforeRevision.toString()} → {item.afterRevision.toString()}</span>
                  </div>
                )) : <p className="border-t border-line p-4 text-xs text-muted-foreground">{t("operator.audit.empty")}</p>}
              </div>
              <div className="border-t border-line">
                <h3 className="p-4 text-sm font-medium">
                  {t("operator.audit.payments")}
                </h3>
                {detail.commerce?.audit
                  .slice(-8)
                  .reverse()
                  .map((item) => (
                    <div
                      className="grid min-w-0 gap-1 border-t border-line px-4 py-3 text-xs sm:grid-cols-[180px_minmax(0,1fr)]"
                      key={item.auditId}
                    >
                      <span className="text-muted-foreground">
                        {new Date(
                          Number(item.occurredAtUnixMillis),
                        ).toLocaleString()}
                      </span>
                      <span className="min-w-0 break-words">{item.action}</span>
                    </div>
                  ))}
              </div>
            </>
          ) : (
            <p className="p-8 text-sm text-muted-foreground">
              {t("operator.detail.select")}
            </p>
          )}
        </section>
      </div>}
      {activeModule === "hubs" && <section className="mt-5 scroll-mt-4 border border-line bg-panel" id="operator-fleet">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line p-4">
          <h2 className="flex items-center gap-2 text-sm font-medium"><Server className="size-4" />{t("operator.fleet.title")}</h2>
          <span className="font-mono text-[10px] text-muted-foreground">{t("operator.fleet.count", { count: fleet?.hubs.length ?? 0 })}</span>
        </div>
        <details className="group border-b border-line" data-testid="hub-create-panel">
          <summary className="flex min-h-11 cursor-pointer list-none items-center gap-2 px-4 py-3 text-xs font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <Plus className="size-4" /> {t("operator.fleet.add")}
          </summary>
          <form className="grid gap-3 border-t border-line p-4 md:grid-cols-2 xl:grid-cols-4" data-requires-recent-auth onSubmit={createHub}>
            {([
              [t("operator.fleet.fields.hubId"), "hubId"], [t("operator.fleet.fields.edgeId"), "edgeDeploymentId"], [t("operator.fleet.fields.relayId"), "relayId"], [t("operator.fleet.fields.region"), "region"], [t("operator.fleet.fields.publicLabel"), "publicLabel"], [t("operator.fleet.fields.publicHubUrl"), "publicHubUrl"], [t("operator.fleet.fields.healthUrl"), "healthUrl"], [t("operator.fleet.fields.capacity"), "maxAssignments"], [t("operator.fleet.fields.hubKey"), "hubControlPublicKey"], [t("operator.fleet.fields.relayKey"), "relayControlPublicKey"], [t("operator.common.changeReason"), "reason"],
            ] as const).map(([label, field]) => (
              <label className={field.includes("PublicKey") || field === "reason" ? "grid gap-1 xl:col-span-2" : "grid gap-1"} key={field}>
                <span className="text-[10px] font-medium text-muted-foreground">{label}</span>
                <Input required min={field === "maxAssignments" ? 1 : undefined} type={field === "maxAssignments" ? "number" : "text"} value={hubForm[field]} onChange={(event) => setHubForm((current) => ({ ...current, [field]: event.target.value }))} />
              </label>
            ))}
            <div className="flex items-end xl:col-span-4">
              <Button className="min-h-11" data-requires-recent-auth disabled={busy} type="submit"><Plus className="size-4" />{t("operator.fleet.create")}</Button>
            </div>
          </form>
        </details>
        <div className="grid gap-3 p-4 lg:grid-cols-2">
          {fleet?.hubs.map((hub) => {
            const deployment = hub.deployment;
            const metadata = deployment?.metadata;
            if (!deployment || !metadata) return null;
            const lifecycle = t(deployment.archived ? "operator.fleet.lifecycle.archived" : !deployment.identityApproved ? "operator.fleet.lifecycle.pendingApproval" : deployment.draining ? "operator.fleet.lifecycle.draining" : deployment.enabled ? "operator.fleet.lifecycle.active" : "operator.fleet.lifecycle.disabled");
            return <article className="border border-line p-4" data-testid={`hub-${metadata.hubId}`} key={metadata.hubId}>
              <div className="flex flex-wrap items-start justify-between gap-2">
                <span><a className="block font-semibold hover:text-primary" href={`/operator/hubs/${encodeURIComponent(metadata.hubId)}`} onClick={(event) => { event.preventDefault(); navigateDetail("hubs", metadata.hubId); }}>{metadata.publicLabel || metadata.hubId}</a><small className={`font-mono text-[10px] ${activeResourceId === metadata.hubId ? "text-primary" : "text-muted-foreground"}`}>{metadata.hubId} · {metadata.region}</small></span>
                <span className={deployment.archived ? "text-muted-foreground" : deployment.draining || !deployment.identityApproved ? "text-warning" : "text-success"}>{lifecycle}</span>
              </div>
              <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                <div><dt className="text-muted-foreground">{t("operator.fleet.assignments")}</dt><dd className="font-mono">{hub.activeAssignments} / {deployment.maxAssignments}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.fleet.directoryRevision")}</dt><dd className="font-mono">{deployment.directoryRevision}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.fleet.hubControl")}</dt><dd>{hub.hubReady ? t("operator.fleet.ready", { generation: hub.hubControlGeneration.toString() }) : t("operator.fleet.notReady")}</dd></div>
                <div><dt className="text-muted-foreground">{t("operator.fleet.relayControl")}</dt><dd>{hub.relayReady ? t("operator.fleet.ready", { generation: hub.relayControlGeneration.toString() }) : t("operator.fleet.notReady")}</dd></div>
              </dl>
              <p className="mt-3 break-all font-mono text-[10px] text-muted-foreground">{deployment.publicHubUrl}<br />{deployment.healthUrl}</p>
              {!deployment.identityApproved && <div className="mt-3 border-t border-line pt-3 text-[10px] text-muted-foreground"><p className="break-all font-mono">Hub: {metadata.hubControlIdentityFingerprint}</p><p className="mt-1 break-all font-mono">Relay: {metadata.relayControlIdentityFingerprint}</p></div>}
              <div className="mt-4 flex flex-wrap gap-2">
                {!deployment.identityApproved && !deployment.archived && <Button className="min-h-11" data-requires-recent-auth disabled={busy} onClick={() => void approveHub(metadata.hubId)}><Check className="size-4" />{t("operator.fleet.approve")}</Button>}
                {deployment.identityApproved && deployment.enabled && !deployment.archived && <Button className="min-h-11" data-requires-recent-auth disabled={busy} variant="outline" onClick={() => void setHubDrain(metadata.hubId, !deployment.draining)}><PauseCircle className="size-4" />{t(deployment.draining ? "operator.fleet.cancelDrain" : "operator.fleet.startDrain")}</Button>}
                {!deployment.archived && <Button className="min-h-11" disabled={busy} variant="outline" onClick={() => beginHubEdit(hub)}><Edit3 className="size-4" />{t("operator.actions.edit")}</Button>}
                {!deployment.archived && deployment.draining && hub.activeAssignments === 0n && <Button className="min-h-11 text-destructive" data-requires-recent-auth disabled={busy} variant="outline" onClick={() => void disableHub(metadata.hubId)}><Power className="size-4" />{t("operator.actions.disable")}</Button>}
              </div>
              {editingHubId === metadata.hubId && <form className="mt-4 grid gap-3 border-t border-line pt-4 sm:grid-cols-2" data-requires-recent-auth onSubmit={updateHub}>
                {([[t("operator.fleet.fields.region"), "region"], [t("operator.fleet.fields.publicLabel"), "publicLabel"], [t("operator.fleet.fields.publicHubUrl"), "publicHubUrl"], [t("operator.fleet.fields.healthUrl"), "healthUrl"], [t("operator.fleet.fields.capacity"), "maxAssignments"], [t("operator.common.changeReason"), "reason"]] as const).map(([label, field]) => <label className="grid gap-1" key={field}><span className="text-[10px] text-muted-foreground">{label}</span><Input required min={field === "maxAssignments" ? 1 : undefined} type={field === "maxAssignments" ? "number" : "text"} value={hubEdit[field]} onChange={(event) => setHubEdit((current) => ({ ...current, [field]: event.target.value }))} /></label>)}
                <div className="flex gap-2 sm:col-span-2"><Button className="min-h-11" data-requires-recent-auth disabled={busy} type="submit">{t("operator.fleet.save")}</Button><Button className="min-h-11" type="button" variant="outline" onClick={() => setEditingHubId("")}>{t("operator.actions.cancel")}</Button></div>
              </form>}
            </article>;
          })}
          {!fleet?.hubs.length && <p className="text-sm text-muted-foreground">{t("operator.fleet.empty")}</p>}
        </div>
      </section>}
      </main>
    </div>
  );
}

function decodeToken(value: string): Uint8Array {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(
    normalized + "=".repeat((4 - (normalized.length % 4)) % 4),
  );
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}
function formatMoney(minor: bigint, currency = "USD", locale?: string) {
  return new Intl.NumberFormat(locale, { style: "currency", currency }).format(Number(minor) / 100);
}
function parseCatalog(value: string): PlanCatalogContract | undefined {
  if (!value.trim()) return undefined;
  try {
    return fromJsonString(PlanCatalogContractSchema, value);
  } catch {
    return undefined;
  }
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
