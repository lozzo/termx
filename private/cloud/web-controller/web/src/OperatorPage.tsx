import { create } from "@bufbuild/protobuf";
import {
  Building2,
  LogOut,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
} from "lucide-react";
import { FormEvent, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  GetOperatorAccountRequestSchema,
  GetOperatorAccountResponseSchema,
  ListHubFleetRequestSchema,
  ListHubFleetResponseSchema,
  ListOperatorAccountsRequestSchema,
  ListOperatorAccountsResponseSchema,
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
  RevokeCloudDeviceTargetSchema,
  type GetOperatorAccountResponse,
  type ListHubFleetResponse,
  type ListOperatorAccountsResponse,
} from "@/generated/cloudpb/cloud_management_pb";
import {
  SubscriptionStatus,
  SubscriptionTransitionKind,
} from "@/generated/cloudpb/cloud_product_pb";
import { Freshness } from "@/generated/cloudpb/cloud_topology_pb";
import { ProtoHTTPError, protoPost } from "@/protoApi";

export default function OperatorPage() {
  const [token, setToken] = useState("");
  const [authenticated, setAuthenticated] = useState(false);
  const [accounts, setAccounts] = useState<ListOperatorAccountsResponse>();
  const [fleet, setFleet] = useState<ListHubFleetResponse>();
  const [detail, setDetail] = useState<GetOperatorAccountResponse>();
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");

  async function load(search = query) {
    try {
      const page = create(PageRequestSchema, { pageSize: 100 });
      const [nextAccounts, nextFleet] = await Promise.all([
        protoPost(
          "/api/v1/operator/accounts/list",
          ListOperatorAccountsRequestSchema,
          create(ListOperatorAccountsRequestSchema, { query: search, page }),
          ListOperatorAccountsResponseSchema,
          "termx_cloud_operator_csrf",
        ),
        protoPost(
          "/api/v1/operator/fleet/list",
          ListHubFleetRequestSchema,
          create(ListHubFleetRequestSchema, { page }),
          ListHubFleetResponseSchema,
          "termx_cloud_operator_csrf",
        ),
      ]);
      setAccounts(nextAccounts);
      setFleet(nextFleet);
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
        "termx_cloud_operator_csrf",
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
      setDetail(
        await protoPost(
          "/api/v1/operator/accounts/get",
          GetOperatorAccountRequestSchema,
          create(GetOperatorAccountRequestSchema, { accountId }),
          GetOperatorAccountResponseSchema,
          "termx_cloud_operator_csrf",
        ),
      );
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Account detail failed",
      );
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
        "termx_cloud_operator_csrf",
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
        "termx_cloud_operator_csrf",
      );
      if (detail?.commerce?.account?.accountId)
        await select(detail.commerce.account.accountId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Device revoke failed");
    }
  }

  async function logout() {
    await protoPost(
      "/api/v1/operator/logout",
      OperatorLogoutRequestSchema,
      create(OperatorLogoutRequestSchema),
      OperatorLogoutResponseSchema,
      "termx_cloud_operator_csrf",
    );
    setAuthenticated(false);
    setAccounts(undefined);
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
            TERMX CLOUD / OPERATOR
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
        <p className="mt-5 border border-destructive p-3 text-xs text-destructive">
          {error}
        </p>
      )}
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
                    onClick={() =>
                      void transition(SubscriptionTransitionKind.SUSPEND)
                    }
                  >
                    Suspend
                  </Button>
                  <Button
                    variant="outline"
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
              <div className="border-t border-line">
                <h3 className="p-4 text-sm font-medium">Devices</h3>
                {detail.devices?.devices.map((device) => (
                  <div
                    className="flex items-center justify-between gap-3 border-t border-line px-4 py-3 text-xs"
                    key={device.deviceId}
                  >
                    <span className="min-w-0">
                      <strong className="block truncate">
                        {device.displayName || device.deviceId}
                      </strong>
                      <small className="font-mono text-muted-foreground">
                        {device.deviceId}
                      </small>
                    </span>
                    <Button
                      variant="outline"
                      disabled={device.revoked}
                      onClick={() =>
                        void revokeDevice(device.deviceId, device.authEpoch)
                      }
                    >
                      Revoke
                    </Button>
                  </div>
                ))}
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
