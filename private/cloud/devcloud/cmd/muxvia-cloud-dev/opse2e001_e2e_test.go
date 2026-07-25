package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/controller"
	"github.com/muxvia/muxvia/private/cloud/edge"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// TestOPSE2E001NineModuleOperatorWorkflow 只负责真实进程、Presence 和重启夹具；九模块 mutation 均由已加载的 Web 页面发起。
func TestOPSE2E001NineModuleOperatorWorkflow(t *testing.T) {
	root := findRepoRoot(t)
	artifactDir := t.TempDir()
	manifestPath := filepath.Join(artifactDir, "runtime.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--manifest", manifestPath, "--repo-root", root, "--fault-harness"}) }()
	var manifest supervisorManifest
	if err := waitManifest(ctx, manifestPath, &manifest, done); err != nil {
		cancel()
		t.Fatal(err)
	}
	var restarted []*childProcess
	defer func() {
		stopChildren(restarted)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("OPSE2E supervisor did not stop")
		}
	}()

	credentials := readDevelopmentCredentials(t, manifest.CredentialsPath)
	operatorClient := operatorHTTPClient(t)
	operatorLoginHTTP(t, operatorClient, manifest.Controller.OperatorURL, credentials.OperatorAccessToken)
	accounts := &cloudpb.ListOperatorAccountsResponse{}
	operatorPost(t, operatorClient, manifest.Controller.OperatorURL, "/api/v1/operator/accounts/list", &cloudpb.ListOperatorAccountsRequest{Page: &cloudpb.PageRequest{PageSize: 10}}, accounts)
	if len(accounts.GetAccounts()) != 1 {
		t.Fatalf("development accounts = %v", accounts)
	}
	accountID := accounts.GetAccounts()[0].GetAccount().GetAccountId()
	authEpoch := accounts.GetAccounts()[0].GetAccount().GetAuthRevision()
	presenceA := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-a", "hub-edge-a", 1)
	presenceA.reportInventory(t, "opse2e-runtime-a", 1)
	presenceB := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-b", "hub-edge-b", 1)
	presenceB.reportInventory(t, "opse2e-runtime-b", 1)
	defer presenceA.Close()
	defer presenceB.Close()

	playwrightLog := filepath.Join(root, ".artifacts", "opse2e001", "playwright.log")
	if err := os.MkdirAll(filepath.Dir(playwrightLog), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("npx", "playwright", "test", "e2e/opse2e001.spec.ts")
	command.Dir = filepath.Join(root, "private", "cloud", "web-controller", "web")
	command.Env = append(os.Environ(), "MUXVIA_OPSE2E_MANIFEST="+manifestPath)
	output, err := command.CombinedOutput()
	if writeErr := os.WriteFile(playwrightLog, output, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil {
		t.Fatalf("Playwright OPSE2E001 failed: %v\n%s", err, output)
	}

	closedContext, cancelClosed := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClosed()
	if event, receiveErr := presenceA.presence.Receive(closedContext); receiveErr == nil {
		t.Fatalf("Web UI Kick did not close daemon Presence: %v", event)
	}
	kick := waitOperatorKickApplied(t, operatorClient, manifest.Controller.OperatorURL, accountID)

	edgeRecord := processByName(t, manifest.Processes, "hub-edge-b")
	oldEdge := edgeByHub(t, manifest.Edges, "hub-edge-b")
	terminatePID(t, edgeRecord.PID)
	if !waitUnavailable(oldEdge.HealthURL+"/healthz", 5*time.Second) {
		t.Fatal("Edge B remained available after restart trigger")
	}
	_ = os.Remove(edgeRecord.ManifestPath)
	restartedEdge, err := startChild(edgeRecord.BinaryPath, edgeRecord.ConfigPath, edgeRecord.ManifestPath, filepath.Join(artifactDir, "hub-edge-b-opse2e-restart.log"), "hub-edge-b-opse2e-restart")
	if err != nil {
		t.Fatal(err)
	}
	restarted = append(restarted, restartedEdge)
	var edgeRuntime edge.Manifest
	if err := waitManifest(ctx, edgeRecord.ManifestPath, &edgeRuntime, restartedEdge.done); err != nil {
		t.Fatal(err)
	}
	if edgeRuntime.PID == oldEdge.PID || edgeRuntime.ControlGeneration <= oldEdge.ControlGeneration {
		t.Fatalf("Edge restart generation = old=%+v new=%+v", oldEdge, edgeRuntime)
	}

	controllerRecord := processByName(t, manifest.Processes, "controller")
	terminatePID(t, controllerRecord.PID)
	if !waitUnavailable(manifest.Controller.OperatorURL+"/healthz", 5*time.Second) {
		t.Fatal("Controller remained available after restart trigger")
	}
	restartOwnedTestPostgres(t)
	_ = os.Remove(controllerRecord.ManifestPath)
	restartedController, err := startChild(controllerRecord.BinaryPath, controllerRecord.ConfigPath, controllerRecord.ManifestPath, filepath.Join(artifactDir, "controller-opse2e-restart.log"), "controller-opse2e-restart")
	if err != nil {
		t.Fatal(err)
	}
	restarted = append(restarted, restartedController)
	var controllerRuntime controller.Manifest
	if err := waitManifest(ctx, controllerRecord.ManifestPath, &controllerRuntime, restartedController.done); err != nil {
		t.Fatal(err)
	}
	if err := waitHealth(ctx, controllerRuntime.OperatorURL+"/healthz", true); err != nil {
		t.Fatal(err)
	}
	waitControlGenerations(t, controllerPostgresDSN(t, manifest), map[string]uint64{"hub-edge-a": 2, "hub-edge-b": 3})

	restartedClient := operatorHTTPClient(t)
	operatorLoginHTTP(t, restartedClient, controllerRuntime.OperatorURL, credentials.OperatorAccessToken)
	evidence := assertNineModuleState(t, restartedClient, controllerRuntime.OperatorURL, accountID, kick.GetCommandId())
	evidence["controller_pid_before"] = manifest.Controller.PID
	evidence["controller_pid_after"] = controllerRuntime.PID
	evidence["edge_b_pid_before"] = oldEdge.PID
	evidence["edge_b_pid_after"] = edgeRuntime.PID
	evidence["postgres_restarted"] = true
	if err := writeJSONFile(filepath.Join(root, ".artifacts", "opse2e001", "runtime-evidence.json"), evidence); err != nil {
		t.Fatal(err)
	}
	scanHUB007RuntimeLogs(t, manifest, credentials, []string{restartedEdge.record.LogPath, restartedController.record.LogPath, playwrightLog})
}

func waitOperatorKickApplied(t *testing.T, client *http.Client, origin, accountID string) *cloudpb.ManagementCommandProjection {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response := &cloudpb.GetOperatorAccountResponse{}
		operatorPost(t, client, origin, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: accountID}, response)
		for _, command := range response.GetCommands() {
			if command.GetCommandKind() == cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_KICK_PRESENCE && (command.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED || command.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED) {
				return command
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Web UI Kick command did not reach APPLIED")
	return nil
}

func restartOwnedTestPostgres(t *testing.T) {
	t.Helper()
	dataDir, binaries := os.Getenv("MUXVIA_TEST_POSTGRES_DATA_DIR"), os.Getenv("MUXVIA_TEST_POSTGRES_BIN")
	if dataDir == "" || binaries == "" {
		t.Fatal("OPSE2E001 requires scripts/with-test-postgres.sh to own the restartable PostgreSQL process")
	}
	stop := exec.Command(filepath.Join(binaries, "pg_ctl"), "-D", dataDir, "stop", "-m", "fast")
	if output, err := stop.CombinedOutput(); err != nil {
		t.Fatalf("stop PostgreSQL: %v: %s", err, output)
	}
	start := exec.Command(filepath.Join(binaries, "pg_ctl"), "-D", dataDir, "-o", "-h 127.0.0.1 -p 55432", "-l", filepath.Join(dataDir, "server.log"), "start")
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("restart PostgreSQL: %v: %s", err, output)
	}
}

func assertNineModuleState(t *testing.T, client *http.Client, origin, accountID, kickCommandID string) map[string]any {
	t.Helper()
	accounts := &cloudpb.ListOperatorAccountsResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/accounts/list", &cloudpb.ListOperatorAccountsRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, accounts)
	orders := &cloudpb.ListOperatorOrdersResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/orders/list", &cloudpb.ListOperatorOrdersRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, orders)
	subscriptions := &cloudpb.ListOperatorSubscriptionsResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/subscriptions/list", &cloudpb.ListOperatorSubscriptionsRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, subscriptions)
	catalog := &cloudpb.ListPlanCatalogReleasesResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/catalog/list", &cloudpb.ListPlanCatalogReleasesRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, catalog)
	fleet := &cloudpb.ListHubFleetResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/fleet/list", &cloudpb.ListHubFleetRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, fleet)
	agents := &cloudpb.ListOperatorAgentsResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/agents/list", &cloudpb.ListOperatorAgentsRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, agents)
	releases := &cloudpb.ListReleaseArtifactsResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/releases/list", &cloudpb.ListReleaseArtifactsRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, releases)
	promotions := &cloudpb.ListPromotionsResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/promotions/list", &cloudpb.ListPromotionsRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, promotions)
	overrides := &cloudpb.ListEntitlementOverridesResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/entitlement-overrides/list", &cloudpb.ListEntitlementOverridesRequest{AccountId: accountID, Page: &cloudpb.PageRequest{PageSize: 20}}, overrides)
	detail := &cloudpb.GetOperatorAccountResponse{}
	operatorPost(t, client, origin, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: accountID}, detail)

	commandApplied := false
	for _, command := range detail.GetCommands() {
		if command.GetCommandId() == kickCommandID && command.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
			commandApplied = true
		}
	}
	if len(accounts.GetAccounts()) == 0 || len(orders.GetOrders()) == 0 || len(subscriptions.GetSubscriptions()) == 0 || len(catalog.GetReleases()) < 2 || len(fleet.GetHubs()) < 3 || len(agents.GetAgents()) < 2 || len(releases.GetArtifacts()) == 0 || len(promotions.GetPromotions()) == 0 || len(overrides.GetOverrides()) == 0 || len(detail.GetOperatorAudit()) == 0 || !commandApplied {
		body, _ := json.Marshal(map[string]any{"accounts": len(accounts.GetAccounts()), "orders": len(orders.GetOrders()), "subscriptions": len(subscriptions.GetSubscriptions()), "catalog": len(catalog.GetReleases()), "hubs": len(fleet.GetHubs()), "agents": len(agents.GetAgents()), "releases": len(releases.GetArtifacts()), "promotions": len(promotions.GetPromotions()), "overrides": len(overrides.GetOverrides()), "audit": len(detail.GetOperatorAudit()), "command_applied": commandApplied})
		t.Fatalf("nine module state did not survive restart: %s", body)
	}
	return map[string]any{"account_count": len(accounts.GetAccounts()), "order_count": len(orders.GetOrders()), "subscription_count": len(subscriptions.GetSubscriptions()), "catalog_release_count": len(catalog.GetReleases()), "hub_count": len(fleet.GetHubs()), "agent_count": len(agents.GetAgents()), "software_release_count": len(releases.GetArtifacts()), "promotion_count": len(promotions.GetPromotions()), "privilege_count": len(overrides.GetOverrides()), "operator_audit_count": len(detail.GetOperatorAudit()), "kick_command_id": kickCommandID, "kick_command_applied": commandApplied}
}
