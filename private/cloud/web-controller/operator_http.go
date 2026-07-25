package webcontroller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

const (
	operatorSessionCookie = "muxvia_cloud_operator"
	operatorCSRFCookie    = "muxvia_cloud_operator_csrf"
)

// OperatorFleetQuery 返回不伪造在线状态的 Hub/Relay attachment 投影。
type OperatorFleetQuery interface {
	ListHubFleet(context.Context, *cloudpb.ListHubFleetRequest) (*cloudpb.ListHubFleetResponse, error)
	GetHubStatus(context.Context, string) (*cloudpb.GetHubStatusResponse, error)
}

// OperatorAPIConfig 装配独立 operator listener 的最小角色、账号和 fleet API。
type OperatorAPIConfig struct {
	AccessToken  []byte
	OperatorID   string
	ActorKind    cloudpb.ManagementActorKind
	Commerce     *commerce.Service
	Catalog      *cloudcatalog.Service
	Overrides    *cloudentitlement.OverrideService
	Promotions   *promotion.Service
	Topology     ManagementTopologyQuery
	Quota        RelayQuotaQuery
	Outbox       *commandoutbox.Service
	Planner      *commandoutbox.Planner
	Fleet        OperatorFleetQuery
	Now          func() time.Time
	SecureCookie bool
}

// OperatorAPIHandler 使用 HttpOnly session、CSRF 与五分钟近期认证保护管理操作。
func OperatorAPIHandler(config OperatorAPIConfig) (http.Handler, error) {
	if len(config.AccessToken) < 32 || config.OperatorID == "" || config.ActorKind != cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_READONLY && config.ActorKind != cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN || config.Commerce == nil || config.Catalog == nil || config.Overrides == nil || config.Promotions == nil || config.Topology == nil || config.Quota == nil || config.Outbox == nil || config.Planner == nil || config.Fleet == nil {
		return nil, commandoutbox.ErrCommandConflict
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	tokenDigest := sha256.Sum256(config.AccessToken)
	sessions := newProofStore(config.Now)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/operator/login", func(w http.ResponseWriter, r *http.Request) {
		request := &cloudpb.OperatorLoginRequest{}
		err := decodeProductProto(r, request)
		candidate := sha256.Sum256(request.GetAccessToken())
		if err == nil && subtle.ConstantTimeCompare(candidate[:], tokenDigest[:]) != 1 {
			err = commerce.ErrUnauthorized
		}
		clear(request.AccessToken)
		var response *cloudpb.OperatorLoginResponse
		if err == nil {
			token, record, issueErr := sessions.issue(config.OperatorID, config.ActorKind, 8*time.Hour)
			err = issueErr
			if issueErr == nil {
				csrf := make([]byte, 24)
				_, issueErr = rand.Read(csrf)
				err = issueErr
				if issueErr == nil {
					http.SetCookie(w, &http.Cookie{Name: operatorSessionCookie, Value: token, Path: "/", MaxAge: 8 * 60 * 60, HttpOnly: true, Secure: config.SecureCookie, SameSite: http.SameSiteStrictMode})
					http.SetCookie(w, &http.Cookie{Name: operatorCSRFCookie, Value: hex.EncodeToString(csrf), Path: "/", MaxAge: 8 * 60 * 60, Secure: config.SecureCookie, SameSite: http.SameSiteStrictMode})
					response = &cloudpb.OperatorLoginResponse{Session: operatorSession(config.OperatorID, config.ActorKind, record)}
				}
				clear(csrf)
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/logout", func(w http.ResponseWriter, r *http.Request) {
		record, token, err := authenticateOperator(r, sessions, config.OperatorID)
		_ = record
		if err == nil && !operatorMutationAllowed(r) {
			err = commerce.ErrUnauthorized
		}
		if err == nil {
			sessions.revoke(token)
		}
		if err == nil {
			for _, cookie := range []http.Cookie{{Name: operatorSessionCookie, Path: "/", HttpOnly: true}, {Name: operatorCSRFCookie, Path: "/"}} {
				cookie.MaxAge = -1
				cookie.Secure = config.SecureCookie
				cookie.SameSite = http.SameSiteStrictMode
				http.SetCookie(w, &cookie)
			}
		}
		writeManagementProto(w, http.StatusOK, &cloudpb.OperatorLogoutResponse{}, err)
	})
	mux.HandleFunc("POST /api/v1/operator/accounts/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListOperatorAccountsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorAccountsResponse
		if err == nil {
			accounts, listErr := config.Commerce.Accounts(r.Context(), boundedPageSize(request.GetPage(), 50))
			err = listErr
			response = &cloudpb.ListOperatorAccountsResponse{Page: &cloudpb.PageResponse{}}
			for _, account := range accounts {
				if request.GetQuery() != "" && !strings.Contains(strings.ToLower(account.GetEmail()+" "+account.GetAccountId()), strings.ToLower(request.GetQuery())) {
					continue
				}
				summary, summaryErr := operatorAccountSummary(r.Context(), config, account)
				if summaryErr != nil {
					err = summaryErr
					break
				}
				if request.GetSubscriptionStatus() == cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED || summary.GetSubscription().GetStatus() == request.GetSubscriptionStatus() {
					response.Accounts = append(response.Accounts, summary)
				}
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.GetOperatorAccountRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetOperatorAccountResponse
		if err == nil {
			response, err = operatorAccountDetail(r.Context(), config, request.GetAccountId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscription/transition", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.OperatorTransitionSubscriptionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.OperatorTransitionSubscriptionResponse
		if err == nil && request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND && request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE {
			err = commandoutbox.ErrCommandConflict
		}
		if err == nil {
			result, transitionErr := config.Commerce.Transition(r.Context(), &cloudpb.TransitionSubscriptionRequest{AccountId: request.GetAccountId(), Transition: request.GetTransition(), ActorId: config.OperatorID})
			err, response = transitionErr, &cloudpb.OperatorTransitionSubscriptionResponse{Result: result}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/orders/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListOperatorOrdersRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorOrdersResponse
		if err == nil {
			response, err = config.Commerce.OperatorOrders(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscriptions/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListOperatorSubscriptionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorSubscriptionsResponse
		if err == nil {
			response, err = config.Commerce.OperatorSubscriptions(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscriptions/adjust", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.CreateSubscriptionAdjustmentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateSubscriptionAdjustmentResponse
		if err == nil {
			response, err = config.Commerce.AdjustSubscription(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/orders/payment-event", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.ApplyOperatorPaymentEventRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ApplyOperatorPaymentEventResponse
		if err == nil {
			response, err = config.Commerce.ApplyOperatorPaymentEvent(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListPromotionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPromotionsResponse
		if err == nil {
			values, listErr := config.Promotions.List(r.Context(), request.GetIncludeDisabled(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPromotionsResponse{Promotions: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/redemptions", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListPromotionRedemptionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPromotionRedemptionsResponse
		if err == nil {
			values, listErr := config.Promotions.Redemptions(r.Context(), request.GetPromotionId(), request.GetAccountId(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPromotionRedemptionsResponse{Redemptions: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/create", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.CreatePromotionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreatePromotionResponse
		if err == nil {
			response, err = config.Promotions.Create(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/disable", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.DisablePromotionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.DisablePromotionResponse
		if err == nil {
			response, err = config.Promotions.Disable(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListPlanCatalogReleasesRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPlanCatalogReleasesResponse
		if err == nil {
			values, listErr := config.Catalog.Releases(r.Context(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPlanCatalogReleasesResponse{Releases: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/get", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.GetPlanCatalogReleaseRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetPlanCatalogReleaseResponse
		if err == nil {
			value, getErr := config.Catalog.Release(r.Context(), request.GetCatalogVersion())
			err, response = getErr, &cloudpb.GetPlanCatalogReleaseResponse{Release: value}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/publish", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.PublishPlanCatalogRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.PublishPlanCatalogResponse
		if err == nil {
			value, publishErr := config.Catalog.Publish(r.Context(), request.GetCatalog(), config.OperatorID, request.GetReason(), request.GetRequestId())
			err, response = publishErr, &cloudpb.PublishPlanCatalogResponse{Release: value}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListEntitlementOverridesRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListEntitlementOverridesResponse
		if err == nil {
			values, listErr := config.Overrides.List(r.Context(), request.GetAccountId(), request.GetIncludeRevoked(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListEntitlementOverridesResponse{Overrides: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/put", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.PutEntitlementOverrideRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.PutEntitlementOverrideResponse
		if err == nil {
			response, err = config.Overrides.Put(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/revoke", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.RevokeEntitlementOverrideRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.RevokeEntitlementOverrideResponse
		if err == nil {
			response, err = config.Overrides.Revoke(r.Context(), request, config.OperatorID)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/commands", func(w http.ResponseWriter, r *http.Request) {
		record, _, err := authenticateOperator(r, sessions, config.OperatorID)
		if err == nil {
			err = requireOperatorMutation(r, record, config.Now().UTC())
		}
		request := &cloudpb.CreateManagementCommandRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateManagementCommandResponse
		if err == nil {
			command, _, createErr := config.Planner.Create(r.Context(), request, &cloudpb.ManagementActorProjection{ActorKind: record.actorKind, ActorId: config.OperatorID, DisplayLabel: config.OperatorID}, config.Now().UTC())
			err, response = createErr, &cloudpb.CreateManagementCommandResponse{Command: command}
		}
		writeManagementProto(w, http.StatusAccepted, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/list", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.ListHubFleetRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListHubFleetResponse
		if err == nil {
			response, err = config.Fleet.ListHubFleet(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/get", func(w http.ResponseWriter, r *http.Request) {
		_, _, err := authenticateOperator(r, sessions, config.OperatorID)
		request := &cloudpb.GetHubStatusRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetHubStatusResponse
		if err == nil {
			response, err = config.Fleet.GetHubStatus(r.Context(), request.GetHubId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}

func operatorSession(operatorID string, kind cloudpb.ManagementActorKind, record proofRecord) *cloudpb.OperatorSessionProjection {
	return &cloudpb.OperatorSessionProjection{OperatorId: operatorID, ActorKind: kind, AuthenticatedAtUnixMillis: record.authenticatedAt.UnixMilli(), ExpiresAtUnixMillis: record.expiresAt.UnixMilli()}
}

func authenticateOperator(r *http.Request, sessions *proofStore, operatorID string) (proofRecord, string, error) {
	cookie, err := r.Cookie(operatorSessionCookie)
	if err != nil {
		return proofRecord{}, "", commerce.ErrUnauthorized
	}
	record, ok := sessions.validate(cookie.Value, operatorID)
	if !ok {
		return proofRecord{}, "", commerce.ErrUnauthorized
	}
	return record, cookie.Value, nil
}

func operatorMutationAllowed(r *http.Request) bool {
	cookie, err := r.Cookie(operatorCSRFCookie)
	return err == nil && sameOrigin(r) && cookie.Value != "" && cookie.Value == r.Header.Get("X-Muxvia-CSRF")
}

func requireOperatorMutation(r *http.Request, record proofRecord, now time.Time) error {
	if record.actorKind != cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN {
		return errOperatorForbidden
	}
	if !operatorMutationAllowed(r) {
		return commerce.ErrUnauthorized
	}
	if now.Sub(record.authenticatedAt) > 5*time.Minute {
		return errRecentAuthenticationRequired
	}
	return nil
}

func operatorAccountSummary(ctx context.Context, config OperatorAPIConfig, account *cloudpb.AccountProjection) (*cloudpb.OperatorAccountSummary, error) {
	commerceValue, err := config.Commerce.AccountCommerce(ctx, account.GetAccountId())
	if err != nil {
		return nil, err
	}
	quota, err := config.Quota.SnapshotForPeriod(ctx, account.GetAccountId(), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveFromUnixMillis()), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveUntilUnixMillis()), commerceValue.GetEntitlement().GetCapability().GetRelay().GetMaxBytesPerPeriod(), config.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &cloudpb.OperatorAccountSummary{Account: account, Subscription: commerceValue.GetSubscription(), Entitlement: commerceValue.GetEntitlement(), RelayQuota: quota.GetPeriod()}, nil
}

func operatorAccountDetail(ctx context.Context, config OperatorAPIConfig, accountID string) (*cloudpb.GetOperatorAccountResponse, error) {
	commerceValue, err := config.Commerce.AccountCommerce(ctx, accountID)
	if err != nil {
		return nil, err
	}
	quota, err := config.Quota.SnapshotForPeriod(ctx, accountID, time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveFromUnixMillis()), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveUntilUnixMillis()), commerceValue.GetEntitlement().GetCapability().GetRelay().GetMaxBytesPerPeriod(), config.Now().UTC())
	if err != nil {
		return nil, err
	}
	devices, err := config.Topology.ListAccountDevices(ctx, accountID, cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_UNSPECIFIED, true, 100)
	if err != nil {
		return nil, err
	}
	presences, sessions, err := config.Topology.ListAccountTopology(ctx, accountID, "", "", cloudpb.Freshness_FRESHNESS_UNSPECIFIED, 100)
	if err != nil {
		return nil, err
	}
	commands, err := config.Outbox.List(ctx, accountID, cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_UNSPECIFIED, cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_UNSPECIFIED, 100)
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetOperatorAccountResponse{Commerce: commerceValue, RelayQuota: quota, Devices: &cloudpb.ListAccountDevicesResponse{Devices: devices, Page: &cloudpb.PageResponse{}}, Topology: &cloudpb.ListAccountTopologyResponse{Presences: presences, PeerSessions: sessions, Page: &cloudpb.PageResponse{}}, Commands: commands}, nil
}

var errOperatorForbidden = errors.New("operator role is forbidden")
