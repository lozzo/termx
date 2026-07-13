package webcontroller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPEntitlementPublisher 把支付结果提交给 loopback Control Plane internal endpoint。
// 只有 Control Plane 成功更新并发布 Hub snapshot 后 Activate 才返回成功。
type HTTPEntitlementPublisher struct {
	Origin string
	Client *http.Client
}

// Activate 实现 EntitlementProjectionPublisher，传递规范化商业 metadata，不传 terminal scope。
func (publisher HTTPEntitlementPublisher) Activate(accountID, planID, orderID string, validUntil time.Time) error {
	body, _ := json.Marshal(map[string]any{"account_id": accountID, "plan_id": planID, "order_id": orderID, "valid_until": validUntil.UTC()})
	client := publisher.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(publisher.Origin, "/")+"/v1/internal/web/entitlements", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-TermX-Internal-Service", "web-controller-staging-v1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("publish Control Plane entitlement: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("publish Control Plane entitlement: status %d", response.StatusCode)
	}
	return nil
}

// CommerceHandler 返回浏览器 BFF JSON surface；它只接受 bearer session，不设置 Cookie。
// Cookie、SameSite 与 CSRF 由同源 Next BFF 层负责。
func CommerceHandler(service *CommerceService, center *UserCenterStore, providers []IdentityProvider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/web/auth/providers", func(writer http.ResponseWriter, request *http.Request) {
		writeCommerceJSON(writer, http.StatusOK, providers, nil)
	})
	mux.HandleFunc("POST /v1/web/login", func(writer http.ResponseWriter, request *http.Request) {
		session, err := service.BeginStagingSession("account-dev-local", "user-dev-local", "dev-local@termx.invalid")
		writeCommerceJSON(writer, http.StatusOK, session, err)
	})
	mux.HandleFunc("POST /v1/web/auth/password/login", func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		err := decodeCommerceJSON(request, &input)
		var session CommerceSession
		if err == nil {
			var profile UserProfile
			profile, err = center.AuthenticatePassword(input.Email, input.Password)
			if err == nil {
				session, err = service.BeginStagingSession(profile.AccountID, profile.UserID, profile.Email)
			}
		}
		writeCommerceJSON(writer, http.StatusOK, session, err)
	})
	mux.HandleFunc("POST /v1/web/auth/password/register", func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Aff      string `json:"aff"`
		}
		err := decodeCommerceJSON(request, &input)
		var session CommerceSession
		if err == nil {
			var profile UserProfile
			profile, err = center.RegisterPasswordAccount(input.Email, input.Password, input.Aff)
			if err == nil {
				session, err = service.BeginStagingSession(profile.AccountID, profile.UserID, profile.Email)
			}
		}
		writeCommerceJSON(writer, http.StatusCreated, session, err)
	})
	mux.HandleFunc("GET /v1/web/account", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		writeCommerceJSON(writer, http.StatusOK, service.AccountView(session), err)
	})
	mux.HandleFunc("POST /v1/web/checkout", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		if err != nil {
			writeCommerceJSON(writer, http.StatusUnauthorized, nil, err)
			return
		}
		var input struct {
			PlanID string `json:"plan_id"`
		}
		err = decodeCommerceJSON(request, &input)
		if err == nil {
			var order Order
			order, err = service.CreateCheckout(session, input.PlanID)
			writeCommerceJSON(writer, http.StatusCreated, order, err)
			return
		}
		writeCommerceJSON(writer, http.StatusBadRequest, nil, err)
	})
	mux.HandleFunc("POST /v1/web/staging/confirm", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		var input struct {
			OrderID string `json:"order_id"`
		}
		if err == nil {
			err = decodeCommerceJSON(request, &input)
		}
		if err == nil {
			var order Order
			order, err = service.ConfirmStagingPayment(session, input.OrderID)
			writeCommerceJSON(writer, http.StatusOK, order, err)
			return
		}
		writeCommerceJSON(writer, http.StatusBadRequest, nil, err)
	})
	mux.HandleFunc("GET /v1/web/center", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		if err != nil {
			writeCommerceJSON(writer, http.StatusUnauthorized, nil, err)
			return
		}
		profile, nodes, referrals, audit, err := center.Snapshot(session.AccountID)
		writeCommerceJSON(writer, http.StatusOK, map[string]any{"profile": profile, "nodes": nodes, "referrals": referrals, "audit": audit, "billing": service.AccountView(session)}, err)
	})
	mux.HandleFunc("PATCH /v1/web/profile", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		var input struct {
			DisplayName string `json:"display_name"`
		}
		if err == nil {
			err = decodeCommerceJSON(request, &input)
		}
		var value UserProfile
		if err == nil {
			value, err = center.UpdateProfile(session.AccountID, input.DisplayName)
		}
		writeCommerceJSON(writer, http.StatusOK, value, err)
	})
	mux.HandleFunc("POST /v1/web/nodes/revoke", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		var input struct {
			NodeID string `json:"node_id"`
		}
		if err == nil {
			err = decodeCommerceJSON(request, &input)
		}
		var value ManagedNode
		if err == nil {
			value, err = center.RevokeNode(session.AccountID, input.NodeID)
		}
		writeCommerceJSON(writer, http.StatusOK, value, err)
	})
	mux.HandleFunc("POST /v1/web/password", func(writer http.ResponseWriter, request *http.Request) {
		session, err := commerceSession(request, service)
		var input struct {
			Current string `json:"current_password"`
			Next    string `json:"new_password"`
		}
		if err == nil {
			err = decodeCommerceJSON(request, &input)
		}
		if err == nil {
			err = center.ChangePassword(session.AccountID, input.Current, input.Next)
		}
		writeCommerceJSON(writer, http.StatusNoContent, nil, err)
	})
	return mux
}

// IdentityProvider 描述登录页可用的外部身份提供商；Configured=false 时必须禁用跳转。
type IdentityProvider struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Configured       bool   `json:"configured"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
}

func commerceSession(request *http.Request, service *CommerceService) (CommerceSession, error) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	return service.Authenticate(strings.TrimPrefix(header, "Bearer "))
}

func decodeCommerceJSON(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeCommerceJSON(writer http.ResponseWriter, status int, value any, err error) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if err != nil {
		if err == ErrCommerceUnauthorized {
			status = http.StatusUnauthorized
		} else if status < 400 {
			status = http.StatusConflict
		}
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
		return
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
