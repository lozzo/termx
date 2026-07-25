// Command muxvia-cloud-bootstrap 为单区域公网 staging 生成一次性部署资产。
//
// 它只负责创建独立 PostgreSQL schema、首个 bootstrap 账号、部署密钥和两个 Edge 配置；
// 不拥有运行时状态，也不替代未来面向任意账号的正式 daemon enrollment 产品入口。
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/private/cloud/controller"
	"github.com/muxvia/muxvia/private/cloud/edge"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

const postgresDSNEnvironment = "MUXVIA_CONTROLLER_POSTGRES_DSN"

var schemaPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type deploymentCredentials struct {
	PublicURL                string `json:"public_url"`
	OperatorURL              string `json:"operator_url"`
	OperatorID               string `json:"operator_id"`
	OperatorAccessToken      string `json:"operator_access_token"`
	BootstrapAccountEmail    string `json:"bootstrap_account_email"`
	BootstrapAccountPassword string `json:"bootstrap_account_password"`
	PrimaryHubURL            string `json:"primary_hub_url"`
	SecondaryHubURL          string `json:"secondary_hub_url"`
	CredentialNotAfter       string `json:"credential_not_after"`
}

type options struct {
	outputDir         string
	catalogPath       string
	schema            string
	publicURL         string
	operatorURL       string
	controllerURL     string
	primaryHubURL     string
	secondaryHubURL   string
	primaryPublicIP   string
	secondaryPublicIP string
	bootstrapEmail    string
	creemEnvironment  string
	creemSuccessURL   string
	validity          time.Duration
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("muxvia-cloud-bootstrap", flag.ContinueOnError)
	value := options{}
	flags.StringVar(&value.outputDir, "output-dir", "", "empty output directory for generated deployment assets")
	flags.StringVar(&value.catalogPath, "catalog", "/opt/muxvia/config/plans.json", "plan catalog used to seed the bootstrap account")
	flags.StringVar(&value.schema, "schema", "muxvia_staging", "new PostgreSQL schema owned by this deployment")
	flags.StringVar(&value.publicURL, "public-url", "https://muxvia.com", "public Controller origin")
	flags.StringVar(&value.operatorURL, "operator-url", "https://operator.muxvia.com", "operator Controller origin")
	flags.StringVar(&value.controllerURL, "controller-url", "https://control.muxvia.com", "Edge-to-Controller control origin")
	flags.StringVar(&value.primaryHubURL, "primary-hub-url", "https://us1.edge.muxvia.com", "primary Edge public Hub origin")
	flags.StringVar(&value.secondaryHubURL, "secondary-hub-url", "https://cn1.edge.muxvia.com:41102", "secondary Edge public Hub origin")
	flags.StringVar(&value.primaryPublicIP, "primary-public-ip", "155.94.155.192", "primary Relay public IPv4")
	flags.StringVar(&value.secondaryPublicIP, "secondary-public-ip", "114.66.58.243", "secondary Relay public IPv4")
	flags.StringVar(&value.bootstrapEmail, "bootstrap-email", "bootstrap@muxvia.com", "initial staging account email")
	flags.StringVar(&value.creemEnvironment, "creem-environment", "", "Creem provider environment: test or production")
	flags.StringVar(&value.creemSuccessURL, "creem-success-url", "https://muxvia.com/account?payment=return", "HTTPS account URL used after Creem checkout")
	validityDays := flags.Int("credential-validity-days", 30, "absolute deployment key window in days")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || value.outputDir == "" || *validityDays < 2 || *validityDays > 365 || !schemaPattern.MatchString(value.schema) {
		return fmt.Errorf("output directory, valid schema and 2-365 credential validity days are required")
	}
	value.validity = time.Duration(*validityDays) * 24 * time.Hour
	if err := validateOrigin(value.publicURL); err != nil {
		return fmt.Errorf("public URL: %w", err)
	}
	for name, origin := range map[string]string{"operator URL": value.operatorURL, "Controller URL": value.controllerURL, "primary Hub URL": value.primaryHubURL, "secondary Hub URL": value.secondaryHubURL} {
		if err := validateOrigin(origin); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := validateCreemOptions(value.creemEnvironment, value.creemSuccessURL); err != nil {
		return err
	}
	baseDSN := strings.TrimSpace(os.Getenv(postgresDSNEnvironment))
	if baseDSN == "" {
		return fmt.Errorf("%s is required", postgresDSNEnvironment)
	}
	deploymentDSN, err := dsnWithSchema(baseDSN, value.schema)
	if err != nil {
		return err
	}
	if err := prepareOutputDirectory(value.outputDir); err != nil {
		return err
	}
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		return err
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		return fmt.Errorf("connect bootstrap PostgreSQL: %w", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+value.schema); err != nil {
		return fmt.Errorf("create bootstrap schema: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+value.schema+" CASCADE")
		}
	}()
	if err := generate(ctx, value, deploymentDSN); err != nil {
		return err
	}
	completed = true
	return nil
}

func generate(ctx context.Context, value options, deploymentDSN string) error {
	now := time.Now().UTC()
	notBefore := now.Add(-time.Hour)
	notAfter := now.Add(value.validity)
	projectionPublic, projectionPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_, daemonControlPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	projectionKeyID, err := randomValue("projection", 12)
	if err != nil {
		return err
	}
	daemonControlKeyID, err := randomValue("daemon-control", 12)
	if err != nil {
		return err
	}
	operatorToken, err := randomValue("", 32)
	if err != nil {
		return err
	}
	accountPassword, err := randomValue("", 24)
	if err != nil {
		return err
	}
	operatorTokenBytes, err := base64.RawURLEncoding.DecodeString(operatorToken)
	if err != nil {
		return err
	}

	store, err := cloudpostgres.Open(ctx, deploymentDSN)
	if err != nil {
		return err
	}
	defer store.Close()
	catalog, err := webcontroller.LoadCatalog(value.catalogPath)
	if err != nil {
		return err
	}
	catalogService, err := cloudcatalog.New(store, time.Now)
	if err != nil {
		return err
	}
	if err := catalogService.Bootstrap(ctx, catalog.Contract()); err != nil {
		return err
	}
	commerce, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalogService, Now: time.Now})
	if err != nil {
		return err
	}
	registered, err := commerce.Register(ctx, &cloudpb.RegisterAccountRequest{Email: value.bootstrapEmail, Password: accountPassword})
	if err != nil || registered.GetSession().GetAccount().GetAccountId() == "" {
		return fmt.Errorf("seed bootstrap account: %w", err)
	}

	type edgeMaterial struct {
		config     edge.Config
		deployment controller.DeploymentConfig
	}
	makeEdge := func(edgeID, region, label, hubID, relayID, publicHubURL, publicIP string) (edgeMaterial, error) {
		hubPublic, hubPrivate, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return edgeMaterial{}, err
		}
		relayPublic, relayPrivate, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return edgeMaterial{}, err
		}
		metadata := &cloudpb.EdgeDeploymentMetadata{
			EdgeDeploymentId: edgeID, Region: region, PublicLabel: label, HubId: hubID,
			HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic),
			RelayId:                       relayID, RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic),
		}
		return edgeMaterial{
			deployment: controller.DeploymentConfig{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic), PublicHubURL: publicHubURL, HealthURL: strings.TrimSuffix(publicHubURL, "/") + "/healthz", MaxAssignments: 10_000},
			config: edge.Config{
				ControllerURL: value.controllerURL, PublicHubURL: publicHubURL,
				HubListen: "127.0.0.1:42101", HealthListen: "127.0.0.1:42102", RelayListen: "0.0.0.0:41003", RelayPublicIP: publicIP,
				UsageOutboxPath: "/var/lib/muxvia/edge/usage.outbox", Metadata: metadata,
				HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(hubPrivate), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(relayPrivate),
				ControllerProjectionKeyID: projectionKeyID, ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic),
				CredentialNotBeforeUnixMillis: notBefore.UnixMilli(), CredentialNotAfterUnixMillis: notAfter.UnixMilli(),
			},
		}, nil
	}
	primary, err := makeEdge("edge-us-sjc-1", "us-west-1", "US West", "hub-us-sjc-1", "relay-us-sjc-1", value.primaryHubURL, value.primaryPublicIP)
	if err != nil {
		return err
	}
	secondary, err := makeEdge("edge-cn-nbo-1", "cn-east-1", "China East", "hub-cn-nbo-1", "relay-cn-nbo-1", value.secondaryHubURL, value.secondaryPublicIP)
	if err != nil {
		return err
	}
	controllerConfig := controller.Config{
		PostgresDSN: deploymentDSN, PublicListen: "127.0.0.1:42001", InternalControlListen: "127.0.0.1:42002", OperatorListen: "127.0.0.1:42003",
		CatalogPath: "/opt/muxvia/config/plans.json", ProjectionKeyID: projectionKeyID, ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate),
		CredentialNotBeforeUnixMillis: notBefore.UnixMilli(), CredentialNotAfterUnixMillis: notAfter.UnixMilli(),
		DaemonControlKeyID: daemonControlKeyID, DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate),
		Deployments: []controller.DeploymentConfig{primary.deployment, secondary.deployment}, EnableTestPaymentProvider: value.creemEnvironment == "",
		DevelopmentMobileHubID: primary.deployment.Metadata.GetHubId(), DevelopmentMobileHubURL: value.primaryHubURL, DevelopmentMobileHubRegion: primary.deployment.Metadata.GetRegion(),
		OperatorID: "bootstrap-admin", OperatorRole: "admin", OperatorAccessTokenBase64: base64.RawStdEncoding.EncodeToString(operatorTokenBytes),
		SecureCookie: true, WebStaticDir: "/opt/muxvia/web",
	}
	if value.creemEnvironment != "" {
		controllerConfig.CreemEnvironment = value.creemEnvironment
		controllerConfig.CreemSuccessURL = value.creemSuccessURL
	}
	clear(operatorTokenBytes)
	assets := map[string]any{
		"controller-config.json":     controllerConfig,
		"edge-primary-config.json":   primary.config,
		"edge-secondary-config.json": secondary.config,
		"credentials.json": deploymentCredentials{
			PublicURL: value.publicURL, OperatorURL: value.operatorURL, OperatorID: "bootstrap-admin", OperatorAccessToken: operatorToken,
			BootstrapAccountEmail: value.bootstrapEmail, BootstrapAccountPassword: accountPassword,
			PrimaryHubURL: value.primaryHubURL, SecondaryHubURL: value.secondaryHubURL, CredentialNotAfter: notAfter.Format(time.RFC3339),
		},
		"companion-manifest.json": httpapi.Manifest{
			Version: httpapi.ManifestVersion, Profile: httpapi.ProfileStagingPublicHTTPS,
			ControlPlaneURL: value.publicURL, HubURL: value.primaryHubURL,
			RelayURL: "turn:" + value.primaryPublicIP + ":41003?transport=udp",
			HubID:    primary.deployment.Metadata.GetHubId(), Region: primary.deployment.Metadata.GetRegion(),
			AccountLabel: value.bootstrapEmail, StartedAtRFC3339: now.Format(time.RFC3339),
		},
	}
	for name, asset := range assets {
		if err := writeJSON(filepath.Join(value.outputDir, name), asset); err != nil {
			return err
		}
	}
	return nil
}

func dsnWithSchema(baseDSN, schema string) (string, error) {
	parsed, err := url.Parse(baseDSN)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		return "", fmt.Errorf("bootstrap PostgreSQL DSN is invalid")
	}
	query := parsed.Query()
	if mode := query.Get("sslmode"); mode != "require" && mode != "verify-ca" && mode != "verify-full" {
		return "", fmt.Errorf("bootstrap PostgreSQL DSN must require TLS")
	}
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func validateOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("HTTPS origin without path, query, fragment or userinfo is required")
	}
	return nil
}

func validateCreemOptions(environment, successURL string) error {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return nil
	}
	if environment != "test" && environment != "production" {
		return fmt.Errorf("Creem environment must be test or production")
	}
	parsed, err := url.Parse(strings.TrimSpace(successURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("Creem success URL must be an HTTPS URL without userinfo or fragment")
	}
	return nil
}

func prepareOutputDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("bootstrap output directory must be empty")
	}
	return nil
}

func randomValue(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(value)
	clear(value)
	if prefix == "" {
		return encoded, nil
	}
	return prefix + "-" + encoded, nil
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
