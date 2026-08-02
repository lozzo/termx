package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AuditEdgeIdentityCertificate records system-issued and Edge-applied identity
// generations without persisting certificate bodies or private material.
func (database *Database) AuditEdgeIdentityCertificate(ctx context.Context, edgeID, stage string, fingerprint []byte, notAfter, now time.Time) error {
	edgeID = strings.TrimSpace(edgeID)
	stage = strings.TrimSpace(stage)
	if database == nil || edgeID == "" || len(fingerprint) != sha256.Size || notAfter.IsZero() || now.IsZero() || stage != "issued" && stage != "applied" && stage != "apply_failed" && stage != "recovery_issued" {
		return errors.New("EdgeIdentity certificate audit input is invalid")
	}
	reason := fmt.Sprintf("stage=%s sha256=%s not_after=%s", stage, strings.ToUpper(hex.EncodeToString(fingerprint)), notAfter.UTC().Format(time.RFC3339))
	_, err := database.pool.Exec(ctx, `INSERT INTO operator_audit_events(audit_id,actor_account_id,action,resource_type,resource_id,reason,result,correlation_id,occurred_at) VALUES($1,NULL,$2,'edge',$3,$4,$5,$6,$7)`, uuid.NewString(), "edge.identity_certificate."+stage, edgeID, reason, stage, uuid.NewString(), now.UTC())
	return err
}
