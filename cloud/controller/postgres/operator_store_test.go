package postgres

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestListOperatorAccountsUsesOneQueryAndMatchesDetails(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	counter := &operatorQueryCounter{}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	database := &Database{pool: pool}
	defer database.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	prefix := "operator-list-" + uuid.NewString()
	accountIDs := make([]string, 0, 3)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, accountID := range accountIDs {
			_, _ = database.pool.Exec(cleanupCtx, `DELETE FROM usage_periods WHERE account_id=$1`, accountID)
			_, _ = database.pool.Exec(cleanupCtx, `DELETE FROM daemons WHERE account_id=$1`, accountID)
			_, _ = database.pool.Exec(cleanupCtx, `DELETE FROM subscriptions WHERE account_id=$1`, accountID)
			_, _ = database.pool.Exec(cleanupCtx, `DELETE FROM account_roles WHERE account_id=$1`, accountID)
			_, _ = database.pool.Exec(cleanupCtx, `DELETE FROM accounts WHERE account_id=$1`, accountID)
		}
	}()

	for index := 0; index < 3; index++ {
		accountID := uuid.NewString()
		accountIDs = append(accountIDs, accountID)
		roles := []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}
		if index == 1 {
			roles = append(roles, cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR)
		}
		createdAt := now.Add(time.Duration(index) * time.Second)
		_, err := database.EnsureBootstrapOperator(ctx, account.Record{
			Profile: &cloudv1.AccountProfile{
				AccountId: accountID, Email: prefix + "-" + uuid.NewString() + "@example.invalid",
				DisplayName: prefix + " account", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE,
				Revision: 1, CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(createdAt),
			},
			PasswordHash: []byte("operator-list-test-verifier"),
			Roles:        roles,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	usageStart, usageEnd := now.Add(-time.Hour), now.Add(time.Hour)
	if _, err := database.pool.Exec(ctx, `UPDATE subscriptions SET plan_id='professional',plan_version=1,updated_at=$1 WHERE account_id=$2`, now, accountIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `INSERT INTO usage_periods(account_id,period_start,period_end,relay_ingress_bytes,relay_egress_bytes,revision,updated_at) VALUES($1,$2,$3,120,240,3,$4)`, accountIDs[1], usageStart, usageEnd, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `INSERT INTO daemons(daemon_id,account_id,display_name,device_id,device_public_key,device_fingerprint,revoked,revision,created_at,updated_at) VALUES($1,$2,'批量查询设备',$3,$4,$5,false,1,$6,$6)`, uuid.NewString(), accountIDs[1], "device-"+uuid.NewString(), make([]byte, 32), "fingerprint-"+uuid.NewString(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE accounts SET state='disabled',revision=revision+1,updated_at=$1 WHERE account_id=$2`, now, accountIDs[2]); err != nil {
		t.Fatal(err)
	}

	counter.count.Store(0)
	values, next, err := database.ListOperatorAccounts(ctx, &cloudv1.PageRequest{PageSize: 25, Query: prefix}, now)
	if err != nil {
		t.Fatal(err)
	}
	if queries := counter.count.Load(); queries != 1 {
		t.Fatalf("ListOperatorAccounts queries=%d, want 1", queries)
	}
	if len(values) != len(accountIDs) || next != "" {
		t.Fatalf("ListOperatorAccounts len=%d next=%q", len(values), next)
	}
	for _, value := range values {
		expected, err := database.GetOperatorAccount(ctx, value.GetAccount().GetAccountId(), now)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(value, expected) {
			t.Fatalf("batch summary mismatch for %s\nbatch: %v\ndetail: %v", value.GetAccount().GetAccountId(), value, expected)
		}
	}
}

type operatorQueryCounter struct {
	count atomic.Int64
}

func (counter *operatorQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.count.Add(1)
	return ctx
}

func (*operatorQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestAuditCursorRoundTripAndRejectsInvalidValues(t *testing.T) {
	at := time.Date(2026, 7, 26, 12, 34, 56, 789, time.FixedZone("CST", 8*60*60))
	id := uuid.NewString()
	decodedAt, decodedID, err := decodeAuditCursor(encodeAuditCursor(at, id))
	if err != nil {
		t.Fatal(err)
	}
	if !decodedAt.Equal(at) || decodedID != id {
		t.Fatalf("decoded cursor = %s %s", decodedAt, decodedID)
	}
	if _, _, err := decodeAuditCursor("not-a-cursor"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}
