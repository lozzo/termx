package apihttp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	accessCookieName  = "anytty_cloud_access"
	refreshCookieName = "anytty_cloud_refresh"
	csrfCookieName    = "anytty_cloud_csrf"
)

func (handler *handler) accountPublic(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/account/login":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		input := &cloudv1.LoginAccountRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Accounts.Login(request.Context(), input)
		if err != nil {
			writeError(writer, http.StatusUnauthorized, err)
			return
		}
		handler.setSessionCookies(writer, response.GetSession())
		response.Session = redactedSession(response.GetSession())
		writeProto(writer, http.StatusOK, response)
	case "/api/account/register":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		input := &cloudv1.RegisterAccountRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Accounts.Register(request.Context(), input)
		if err != nil {
			writeError(writer, http.StatusConflict, err)
			return
		}
		handler.setSessionCookies(writer, response.GetSession())
		response.Session = redactedSession(response.GetSession())
		writeProto(writer, http.StatusCreated, response)
	case "/api/account/refresh":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cookie, err := request.Cookie(refreshCookieName)
		if err != nil {
			writeError(writer, http.StatusUnauthorized, account.ErrUnauthenticated)
			return
		}
		token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err != nil {
			writeError(writer, http.StatusUnauthorized, account.ErrUnauthenticated)
			return
		}
		response, err := handler.config.Accounts.Refresh(request.Context(), &cloudv1.RefreshAccountSessionRequest{RefreshToken: token})
		if err != nil {
			writeError(writer, http.StatusUnauthorized, err)
			return
		}
		handler.setSessionCookies(writer, response.GetSession())
		response.Session = redactedSession(response.GetSession())
		writeProto(writer, http.StatusOK, response)
	default:
		http.NotFound(writer, request)
	}
}

func (handler *handler) accountPrivate(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/account/current":
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		response, err := handler.config.Accounts.GetCurrent(request.Context(), &cloudv1.GetCurrentAccountRequest{})
		writeServiceResult(writer, response, err)
	case "/api/account/recent-auth":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		input := &cloudv1.VerifyRecentAuthenticationRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Accounts.VerifyRecentAuthentication(request.Context(), input)
		writeServiceResult(writer, response, err)
	case "/api/account/logout":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		identity, _ := account.IdentityFromContext(request.Context())
		response, err := handler.config.Accounts.Logout(request.Context(), &cloudv1.LogoutAccountSessionRequest{SessionId: identity.SessionID})
		if err != nil {
			writeError(writer, http.StatusConflict, err)
			return
		}
		handler.clearSessionCookies(writer)
		writeProto(writer, http.StatusOK, response)
	case "/api/account/sessions":
		switch request.Method {
		case http.MethodGet:
			response, err := handler.config.Accounts.ListSessions(request.Context(), &cloudv1.ListAccountSessionsRequest{})
			writeServiceResult(writer, response, err)
		case http.MethodDelete:
			input := &cloudv1.RevokeAccountSessionRequest{}
			if err := readProto(request, input); err != nil {
				writeError(writer, http.StatusBadRequest, err)
				return
			}
			response, err := handler.config.Accounts.RevokeSession(request.Context(), input)
			writeServiceResult(writer, response, err)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "/api/account/password":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		input := &cloudv1.ChangeAccountPasswordRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Accounts.ChangePassword(request.Context(), input)
		writeServiceResult(writer, response, err)
	default:
		http.NotFound(writer, request)
	}
}

func (handler *handler) commerce(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.URL.Path == "/api/commerce/plans" && request.Method == http.MethodGet:
		response, err := handler.config.Commerce.ListPlans(request.Context(), &cloudv1.ListPlansRequest{})
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/commerce/order" && request.Method == http.MethodPost:
		input := &cloudv1.CreateOrderRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Commerce.CreateOrder(request.Context(), input)
		writeServiceResult(writer, response, err)
	case strings.HasPrefix(request.URL.Path, "/api/commerce/account/") && request.Method == http.MethodGet:
		response, err := handler.config.Commerce.GetAccountCommerce(request.Context(), &cloudv1.GetAccountCommerceRequest{AccountId: strings.TrimPrefix(request.URL.Path, "/api/commerce/account/")})
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/commerce/me" && request.Method == http.MethodGet:
		response, err := handler.config.Commerce.GetMyCommerce(request.Context(), &cloudv1.GetMyCommerceRequest{})
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/commerce/orders" && request.Method == http.MethodPost:
		input := &cloudv1.CreateMyOrderRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Commerce.CreateMyOrder(request.Context(), input)
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/commerce/subscription" && request.Method == http.MethodPost:
		input := &cloudv1.ChangeMySubscriptionRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Commerce.ChangeMySubscription(request.Context(), input)
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/commerce/payments/development" && request.Method == http.MethodPost:
		input := &cloudv1.CompleteDevelopmentPaymentRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.Commerce.CompleteDevelopmentPayment(request.Context(), input)
		writeServiceResult(writer, response, err)
	default:
		http.NotFound(writer, request)
	}
}

func (handler *handler) daemons(writer http.ResponseWriter, request *http.Request) {
	if handler.config.DaemonManagement == nil {
		http.NotFound(writer, request)
		return
	}
	switch {
	case request.URL.Path == "/api/daemons" && request.Method == http.MethodGet:
		response, err := handler.config.DaemonManagement.ListMyDaemons(request.Context(), &cloudv1.ListMyDaemonsRequest{})
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/daemons/enroll" && request.Method == http.MethodPost:
		input := &cloudv1.CreateMyDaemonEnrollmentRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		response, err := handler.config.DaemonManagement.CreateMyEnrollment(request.Context(), input)
		writeServiceResult(writer, response, err)
	case strings.HasPrefix(request.URL.Path, "/api/daemons/") && strings.HasSuffix(request.URL.Path, "/revoke") && request.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/daemons/"), "/revoke")
		input := &cloudv1.RevokeMyDaemonRequest{DaemonId: id}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		input.DaemonId = id
		response, err := handler.config.DaemonManagement.RevokeMyDaemon(request.Context(), input)
		writeServiceResult(writer, response, err)
	default:
		http.NotFound(writer, request)
	}
}

func (handler *handler) operatorR7(writer http.ResponseWriter, request *http.Request) bool {
	path := strings.TrimPrefix(request.URL.Path, "/api/operator")
	switch {
	case path == "/overview" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.GetOverview(request.Context(), &cloudv1.GetOperatorOverviewRequest{})
		writeServiceResult(writer, response, err)
		return true
	case (path == "/connections" || path == "/sessions") && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListRuntimeSessions(request.Context(), &cloudv1.ListRuntimeSessionsRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case path == "/accounts" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListAccounts(request.Context(), &cloudv1.ListOperatorAccountsRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case strings.HasPrefix(path, "/accounts/"):
		return handler.operatorAccount(writer, request, strings.TrimPrefix(path, "/accounts/"))
	case path == "/plans" && request.Method == http.MethodGet:
		response, err := handler.config.Commerce.ListPlans(request.Context(), &cloudv1.ListPlansRequest{IncludeUnpublished: true})
		writeServiceResult(writer, response, err)
		return true
	case path == "/plans" && request.Method == http.MethodPost:
		input := &cloudv1.CreatePlanVersionRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		response, err := handler.config.Commerce.CreatePlanVersion(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case strings.HasPrefix(path, "/plans/") && strings.HasSuffix(path, "/publish") && request.Method == http.MethodPost:
		parts := strings.Split(strings.Trim(strings.TrimSuffix(strings.TrimPrefix(path, "/plans/"), "/publish"), "/"), "/")
		if len(parts) != 2 {
			writeError(writer, http.StatusBadRequest, errors.New("plan path is invalid"))
			return true
		}
		version, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input := &cloudv1.PublishPlanVersionRequest{PlanId: parts[0], Version: version}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.PlanId = parts[0]
		input.Version = version
		response, err := handler.config.Commerce.PublishPlanVersion(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case path == "/orders" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListOrders(request.Context(), &cloudv1.ListOperatorOrdersRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case path == "/payments/events" && request.Method == http.MethodPost:
		input := &cloudv1.ApplyPaymentEventRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		response, err := handler.config.Commerce.ApplyPaymentEvent(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case path == "/subscriptions" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListSubscriptions(request.Context(), &cloudv1.ListOperatorSubscriptionsRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case path == "/subscriptions/transition" && request.Method == http.MethodPost:
		input := &cloudv1.TransitionSubscriptionRequest{}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		response, err := handler.config.Commerce.TransitionSubscription(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case path == "/usage" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListUsage(request.Context(), &cloudv1.ListOperatorUsageRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case path == "/audit" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListAudit(request.Context(), &cloudv1.ListOperatorAuditRequest{Page: pageRequest(request)})
		writeServiceResult(writer, response, err)
		return true
	case path == "/certificates" && request.Method == http.MethodGet:
		response, err := handler.config.Operator.ListCertificateProfiles(request.Context(), &cloudv1.ListCertificateProfilesRequest{})
		writeServiceResult(writer, response, err)
		return true
	case path == "/certificates" && request.Method == http.MethodPost:
		input := &cloudv1.UploadCertificateProfileRequest{}
		if err := readProtoLimit(request, input, 4<<20); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		response, err := handler.config.Operator.UploadCertificateProfile(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case strings.HasPrefix(path, "/certificates/") && request.Method == http.MethodPut:
		profileID := strings.TrimPrefix(path, "/certificates/")
		input := &cloudv1.UploadCertificateProfileRequest{CertificateProfileId: profileID}
		if err := readProtoLimit(request, input, 4<<20); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.CertificateProfileId = profileID
		response, err := handler.config.Operator.UploadCertificateProfile(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case strings.HasPrefix(path, "/edges/") && strings.HasSuffix(path, "/certificate") && request.Method == http.MethodPost:
		edgeID := strings.TrimSuffix(strings.TrimPrefix(path, "/edges/"), "/certificate")
		input := &cloudv1.BindCertificateProfileRequest{EdgeId: edgeID}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.EdgeId = edgeID
		response, err := handler.config.Operator.BindCertificateProfile(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case path == "/events" && request.Method == http.MethodGet:
		handler.operatorEvents(writer, request)
		return true
	case strings.HasPrefix(path, "/daemons/") && strings.HasSuffix(path, "/disconnect") && request.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/daemons/"), "/disconnect")
		input := &cloudv1.DisconnectDaemonRequest{DaemonId: id}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.DaemonId = id
		response, err := handler.config.Operator.DisconnectDaemon(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case strings.HasPrefix(path, "/connections/") && strings.HasSuffix(path, "/disconnect") && request.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/connections/"), "/disconnect")
		input := &cloudv1.DisconnectSessionRequest{SessionId: id}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.SessionId = id
		response, err := handler.config.Operator.DisconnectSession(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	}
	return false
}

func (handler *handler) operatorAccount(writer http.ResponseWriter, request *http.Request, suffix string) bool {
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 1 && request.Method == http.MethodGet {
		response, err := handler.config.Operator.GetAccount(request.Context(), &cloudv1.GetOperatorAccountRequest{AccountId: parts[0]})
		writeServiceResult(writer, response, err)
		return true
	}
	if len(parts) != 2 || request.Method != http.MethodPost {
		return false
	}
	switch parts[1] {
	case "state":
		input := &cloudv1.SetAccountStateRequest{AccountId: parts[0]}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.AccountId = parts[0]
		response, err := handler.config.Operator.SetAccountState(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	case "role":
		input := &cloudv1.SetAccountRoleRequest{AccountId: parts[0]}
		if err := readProto(request, input); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return true
		}
		input.AccountId = parts[0]
		response, err := handler.config.Operator.SetAccountRole(request.Context(), input)
		writeServiceResult(writer, response, err)
		return true
	}
	return false
}

func (handler *handler) operatorEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, errors.New("SSE is unavailable"))
		return
	}
	events, cancel, err := handler.config.Directory.Watch(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, err)
		return
	}
	defer cancel()
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprintf(writer, "event: ready\ndata: {\"controller_instance_id\":%q}\n\n", handler.config.Directory.InstanceID())
	flusher.Flush()
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-keepalive.C:
			_, _ = fmt.Fprint(writer, ": keepalive\n\n")
			flusher.Flush()
		case event, open := <-events:
			if !open {
				return
			}
			payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(event)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(writer, "id: %s:%d\nevent: runtime\ndata: %s\n\n", event.GetControllerInstanceId(), event.GetEventSeq(), payload)
			flusher.Flush()
		}
	}
}

func (handler *handler) authenticate(writer http.ResponseWriter, request *http.Request) (account.Identity, bool) {
	var encoded string
	if cookie, err := request.Cookie(accessCookieName); err == nil {
		encoded = cookie.Value
	} else if authorization := request.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		encoded = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(token) == 0 {
		if writer != nil {
			writeError(writer, http.StatusUnauthorized, account.ErrUnauthenticated)
		}
		return account.Identity{}, false
	}
	identity, err := handler.config.Accounts.AuthenticateAccess(request.Context(), token)
	if err != nil {
		if writer != nil {
			writeError(writer, http.StatusUnauthorized, err)
		}
		return account.Identity{}, false
	}
	return identity, true
}
func (handler *handler) setSessionCookies(writer http.ResponseWriter, session *cloudv1.AccountSessionCredential) {
	if session == nil {
		return
	}
	http.SetCookie(writer, &http.Cookie{Name: accessCookieName, Value: base64.RawURLEncoding.EncodeToString(session.GetAccessToken()), Path: "/", Expires: session.GetAccessExpiresAt().AsTime(), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(writer, &http.Cookie{Name: refreshCookieName, Value: base64.RawURLEncoding.EncodeToString(session.GetRefreshToken()), Path: "/api/account/refresh", Expires: session.GetRefreshExpiresAt().AsTime(), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(writer, &http.Cookie{Name: csrfCookieName, Value: base64.RawURLEncoding.EncodeToString(session.GetCsrfToken()), Path: "/", Expires: session.GetRefreshExpiresAt().AsTime(), Secure: true, SameSite: http.SameSiteStrictMode})
}
func (handler *handler) clearSessionCookies(writer http.ResponseWriter) {
	for _, cookie := range []http.Cookie{{Name: accessCookieName, Path: "/", HttpOnly: true}, {Name: refreshCookieName, Path: "/api/account/refresh", HttpOnly: true}, {Name: csrfCookieName, Path: "/"}} {
		cookie.Value = ""
		cookie.MaxAge = -1
		cookie.Secure = true
		cookie.SameSite = http.SameSiteStrictMode
		http.SetCookie(writer, &cookie)
	}
}
func redactedSession(value *cloudv1.AccountSessionCredential) *cloudv1.AccountSessionCredential {
	if value == nil {
		return nil
	}
	return &cloudv1.AccountSessionCredential{SessionId: value.GetSessionId(), AccessExpiresAt: value.GetAccessExpiresAt(), RefreshExpiresAt: value.GetRefreshExpiresAt()}
}
func validateEncodedCSRF(identity account.Identity, value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && account.ValidateCSRF(identity, decoded)
}
func decodeBearer(value string) ([]byte, error) {
	if !strings.HasPrefix(value, "Bearer ") {
		return nil, errors.New("bearer token is required")
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")))
}
func pageRequest(request *http.Request) *cloudv1.PageRequest {
	size := uint64(50)
	if parsed, err := strconv.ParseUint(request.URL.Query().Get("page_size"), 10, 32); err == nil && parsed > 0 {
		size = parsed
	}
	return &cloudv1.PageRequest{PageSize: uint32(size), Cursor: request.URL.Query().Get("cursor"), Query: request.URL.Query().Get("query"), Sort: request.URL.Query().Get("sort")}
}
func writeServiceResult(writer http.ResponseWriter, response proto.Message, err error) {
	if err != nil {
		writeError(writer, serviceHTTPStatus(err), err)
		return
	}
	writeProto(writer, http.StatusOK, response)
}
func serviceHTTPStatus(err error) int {
	switch {
	case errors.Is(err, account.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, account.ErrAccountDisabled):
		return http.StatusForbidden
	default:
		return http.StatusConflict
	}
}
