package webcontroller

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	browserSessionCookie = "termx_web_session"
	browserCSRFCookie    = "termx_csrf"
)

// BrowserConfig 配置由 Control Plane 直接提供的同源浏览器 API。
// Session bearer 只存在于 HttpOnly Cookie，React 静态站不能读取或转发它。
type BrowserConfig struct {
	// Catalog 是 Control Plane 读取并验证后的公开套餐真值。
	Catalog *Catalog
	// Commerce 是订单、Session、付款幂等和订阅有效期的领域 owner。
	Commerce *CommerceService
	// UserCenter 是账号、身份、AFF、节点和审计的数据库 owner。
	UserCenter *UserCenterStore
	// IdentityProviders 只公开可用状态和跳转地址，不包含 OAuth secret。
	IdentityProviders []IdentityProvider
	// RelayURL 是只读运行 metadata，不用于浏览器建立 terminal transport。
	RelayURL string
	// StagingLogin 只在显式 staging profile 开启固定开发账号和测试付款。
	StagingLogin bool
	// SecureCookie 控制 Session Cookie 是否只允许 HTTPS；生产必须为 true。
	SecureCookie bool
	// DeviceAccess 把浏览器审批和账号节点 enrollment 写回 Control Plane owner。
	// 浏览器层只传递已认证账号身份，不签发 edge credential，也不接触 daemon capability。
	DeviceAccess DeviceAccessService
}

// DeviceLoginRequest 是浏览器可见的设备码投影；它不包含 flow ID、edge token 或客户端私钥。
type DeviceLoginRequest struct {
	UserCode  string    `json:"user_code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// EnrollmentCode 是账号创建的短期 daemon 注册凭据；Code 只在创建响应中返回一次。
type EnrollmentCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// DeviceAccessService 是 Web 账号 Session 与 Control Plane 设备授权状态机之间的唯一写边界。
// 实现负责过期、重放和账号归属校验；Hub 只消费其后发布的签名授权投影。
type DeviceAccessService interface {
	InspectDeviceLogin(userCode string) (DeviceLoginRequest, error)
	ApproveDeviceLogin(userCode, accountID string) error
	CreateEnrollmentCode(accountID, userID string) (EnrollmentCode, error)
}

// BrowserHandler 返回 `/api/*` 浏览器 surface；它直接拥有 Cookie、Origin 和 CSRF 校验，
// 不经过 Next.js BFF，也不暴露 internal entitlement 或 edge credential API。
func BrowserHandler(config BrowserConfig) (http.Handler, error) {
	if config.Catalog == nil || config.Commerce == nil || config.UserCenter == nil || strings.TrimSpace(config.RelayURL) == "" {
		return nil, ErrCommerceConflict
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeCommerceJSON(w, http.StatusOK, map[string]any{"control_plane_ready": true, "hub_ready": true, "relay": config.RelayURL}, nil)
	})
	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(config.Catalog)
	})
	mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) {
		writeCommerceJSON(w, http.StatusOK, config.IdentityProviders, nil)
	})
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeCommerceJSON(w, http.StatusForbidden, nil, ErrCommerceUnauthorized)
			return
		}
		if !config.StagingLogin {
			writeCommerceJSON(w, http.StatusNotFound, nil, ErrCommerceUnauthorized)
			return
		}
		session, err := config.Commerce.BeginStagingSession("account-dev-local", "user-dev-local", "dev-local@termx.invalid")
		if err == nil {
			err = setBrowserSession(w, session, config.SecureCookie)
		}
		writeCommerceJSON(w, http.StatusOK, map[string]string{"email": session.Email}, err)
	})
	mux.HandleFunc("POST /api/auth/password/login", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeCommerceJSON(w, http.StatusForbidden, nil, ErrCommerceUnauthorized)
			return
		}
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		err := decodeCommerceJSON(r, &input)
		var profile UserProfile
		var session CommerceSession
		if err == nil {
			profile, err = config.UserCenter.AuthenticatePassword(input.Email, input.Password)
		}
		if err == nil {
			session, err = config.Commerce.BeginStagingSession(profile.AccountID, profile.UserID, profile.Email)
		}
		if err == nil {
			err = setBrowserSession(w, session, config.SecureCookie)
		}
		writeCommerceJSON(w, http.StatusOK, map[string]string{"email": session.Email}, err)
	})
	mux.HandleFunc("POST /api/auth/password/register", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeCommerceJSON(w, http.StatusForbidden, nil, ErrCommerceUnauthorized)
			return
		}
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Aff      string `json:"aff"`
		}
		err := decodeCommerceJSON(r, &input)
		var profile UserProfile
		var session CommerceSession
		if err == nil {
			profile, err = config.UserCenter.RegisterPasswordAccount(input.Email, input.Password, input.Aff)
		}
		if err == nil {
			session, err = config.Commerce.BeginStagingSession(profile.AccountID, profile.UserID, profile.Email)
		}
		if err == nil {
			err = setBrowserSession(w, session, config.SecureCookie)
		}
		writeCommerceJSON(w, http.StatusCreated, map[string]string{"email": session.Email}, err)
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !browserMutationAllowed(r) {
			writeCommerceJSON(w, http.StatusForbidden, nil, ErrCommerceUnauthorized)
			return
		}
		if cookie, err := r.Cookie(browserSessionCookie); err == nil {
			config.Commerce.EndSession(cookie.Value)
		}
		clearBrowserSession(w, config.SecureCookie)
		writeCommerceJSON(w, http.StatusOK, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("GET /api/center", func(w http.ResponseWriter, r *http.Request) {
		session, err := browserSession(r, config.Commerce)
		if err != nil {
			writeCommerceJSON(w, http.StatusUnauthorized, nil, err)
			return
		}
		profile, nodes, referrals, audit, err := config.UserCenter.Snapshot(session.AccountID)
		writeCommerceJSON(w, http.StatusOK, map[string]any{"profile": profile, "nodes": nodes, "referrals": referrals, "audit": audit, "billing": config.Commerce.AccountView(session)}, err)
	})
	mux.HandleFunc("GET /api/device-login", func(w http.ResponseWriter, r *http.Request) {
		if config.DeviceAccess == nil {
			writeCommerceJSON(w, http.StatusNotFound, nil, ErrUserCenterNotFound)
			return
		}
		if _, err := browserSession(r, config.Commerce); err != nil {
			writeCommerceJSON(w, http.StatusUnauthorized, nil, err)
			return
		}
		flow, err := config.DeviceAccess.InspectDeviceLogin(r.URL.Query().Get("code"))
		writeCommerceJSON(w, http.StatusOK, flow, err)
	})
	mux.HandleFunc("POST /api/device-login/approve", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			UserCode string `json:"user_code"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		if err == nil && config.DeviceAccess == nil {
			err = ErrUserCenterNotFound
		}
		if err == nil {
			err = config.DeviceAccess.ApproveDeviceLogin(input.UserCode, session.AccountID)
		}
		writeCommerceJSON(w, http.StatusOK, map[string]bool{"approved": err == nil}, err)
	})
	mux.HandleFunc("POST /api/nodes/enrollment", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		if err == nil && config.DeviceAccess == nil {
			err = ErrUserCenterNotFound
		}
		var code EnrollmentCode
		if err == nil {
			code, err = config.DeviceAccess.CreateEnrollmentCode(session.AccountID, session.UserID)
		}
		writeCommerceJSON(w, http.StatusCreated, code, err)
	})
	mux.HandleFunc("POST /api/checkout", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			PlanID string `json:"plan_id"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		var order Order
		if err == nil {
			order, err = config.Commerce.CreateCheckout(session, input.PlanID)
		}
		writeCommerceJSON(w, http.StatusCreated, order, err)
	})
	mux.HandleFunc("POST /api/checkout/confirm", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			OrderID string `json:"order_id"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		var order Order
		if err == nil {
			order, err = config.Commerce.ConfirmStagingPayment(session, input.OrderID)
		}
		writeCommerceJSON(w, http.StatusOK, order, err)
	})
	mux.HandleFunc("PATCH /api/profile", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			DisplayName string `json:"display_name"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		var profile UserProfile
		if err == nil {
			profile, err = config.UserCenter.UpdateProfile(session.AccountID, input.DisplayName)
		}
		writeCommerceJSON(w, http.StatusOK, profile, err)
	})
	mux.HandleFunc("POST /api/nodes/revoke", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			NodeID string `json:"node_id"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		var node ManagedNode
		if err == nil {
			node, err = config.UserCenter.RevokeNode(session.AccountID, input.NodeID)
		}
		writeCommerceJSON(w, http.StatusOK, node, err)
	})
	mux.HandleFunc("POST /api/password", func(w http.ResponseWriter, r *http.Request) {
		session, err := authorizedMutation(r, config.Commerce)
		var input struct {
			Current string `json:"current_password"`
			Next    string `json:"new_password"`
		}
		if err == nil {
			err = decodeCommerceJSON(r, &input)
		}
		if err == nil {
			err = config.UserCenter.ChangePassword(session.AccountID, input.Current, input.Next)
		}
		if err != nil {
			writeCommerceJSON(w, http.StatusConflict, nil, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	return mux, nil
}

func setBrowserSession(w http.ResponseWriter, session CommerceSession, secure bool) error {
	csrf := make([]byte, 24)
	if _, err := rand.Read(csrf); err != nil {
		return err
	}
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	http.SetCookie(w, &http.Cookie{Name: browserSessionCookie, Value: session.Token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: browserCSRFCookie, Value: hex.EncodeToString(csrf), Path: "/", MaxAge: maxAge, Secure: secure, SameSite: http.SameSiteStrictMode})
	return nil
}
func clearBrowserSession(w http.ResponseWriter, secure bool) {
	for _, name := range []string{browserSessionCookie, browserCSRFCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: "/", MaxAge: -1, HttpOnly: name == browserSessionCookie, Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}
func browserSession(r *http.Request, service *CommerceService) (CommerceSession, error) {
	cookie, err := r.Cookie(browserSessionCookie)
	if err != nil {
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	return service.Authenticate(cookie.Value)
}
func authorizedMutation(r *http.Request, service *CommerceService) (CommerceSession, error) {
	if !browserMutationAllowed(r) {
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	return browserSession(r, service)
}
func browserMutationAllowed(r *http.Request) bool {
	csrfCookie, err := r.Cookie(browserCSRFCookie)
	return err == nil && sameOrigin(r) && csrfCookie.Value != "" && csrfCookie.Value == r.Header.Get("X-TermX-CSRF")
}
func sameOrigin(r *http.Request) bool {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return host != "" && r.Header.Get("Origin") == scheme+"://"+host
}
