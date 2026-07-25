package webcontroller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	productAccessCookie  = "muxvia_cloud_access"
	productRefreshCookie = "muxvia_cloud_refresh"
	productCSRFCookie    = "muxvia_cloud_csrf"
)

// ProductAPIConfig 配置 Controller 账号与交易 Proto JSON surface。
type ProductAPIConfig struct {
	Commerce                  *commerce.Service
	SecureCookie              bool
	EnableTestPaymentProvider bool
	CheckoutProvider          CheckoutProvider
}

// CheckoutProvider 把已创建的 pending order 交给正式 provider，并只返回持久 attempt 与跳转 URL。
type CheckoutProvider interface {
	StartCheckout(context.Context, *cloudpb.AccountProjection, *cloudpb.OrderProjection) (*cloudpb.PaymentAttemptProjection, string, error)
}

// ProductAPIHandler 把 generated Cloud Product API 接到 Controller public listener。
// Cookie 只承载 token，账号、订单、Subscription 和 Entitlement 真值全部留在 commerce service。
func ProductAPIHandler(config ProductAPIConfig) (http.Handler, error) {
	if config.Commerce == nil {
		return nil, commerce.ErrConflict
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/account/register", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeProductError(w, http.StatusForbidden, commerce.ErrUnauthorized)
			return
		}
		request := &cloudpb.RegisterAccountRequest{}
		if err := decodeProductProto(r, request); err != nil {
			writeProductError(w, http.StatusBadRequest, commerce.ErrConflict)
			return
		}
		response, err := config.Commerce.Register(r.Context(), request)
		if err == nil {
			err = setProductSession(w, response.GetSession(), config.SecureCookie)
		}
		writeProductProto(w, http.StatusCreated, sanitizeCredentialResponse(response), err)
	})
	mux.HandleFunc("POST /api/v1/account/login", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeProductError(w, http.StatusForbidden, commerce.ErrUnauthorized)
			return
		}
		request := &cloudpb.PasswordLoginRequest{}
		if err := decodeProductProto(r, request); err != nil {
			writeProductError(w, http.StatusBadRequest, commerce.ErrUnauthorized)
			return
		}
		response, err := config.Commerce.Login(r.Context(), request)
		if err == nil {
			err = setProductSession(w, response.GetSession(), config.SecureCookie)
		}
		writeProductProto(w, http.StatusOK, sanitizeLoginResponse(response), err)
	})
	mux.HandleFunc("POST /api/v1/account/refresh", func(w http.ResponseWriter, r *http.Request) {
		if !productMutationAllowed(r) {
			writeProductError(w, http.StatusForbidden, commerce.ErrUnauthorized)
			return
		}
		cookie, err := r.Cookie(productRefreshCookie)
		if err != nil {
			writeProductError(w, http.StatusUnauthorized, commerce.ErrUnauthorized)
			return
		}
		token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err != nil {
			writeProductError(w, http.StatusUnauthorized, commerce.ErrUnauthorized)
			return
		}
		response, err := config.Commerce.Refresh(r.Context(), &cloudpb.RefreshAccountSessionRequest{RefreshToken: token})
		if err == nil {
			err = setProductSession(w, response.GetSession(), config.SecureCookie)
		}
		writeProductProto(w, http.StatusOK, sanitizeRefreshResponse(response), err)
	})
	mux.HandleFunc("POST /api/v1/account/logout", func(w http.ResponseWriter, r *http.Request) {
		account, sessionID, err := authorizeProductMutation(r, config.Commerce)
		if err == nil {
			_, err = config.Commerce.Logout(r.Context(), account.GetAccountId(), account.GetUserId(), &cloudpb.LogoutAccountSessionRequest{SessionId: sessionID})
		}
		if err == nil {
			clearProductSession(w, config.SecureCookie)
		}
		writeProductProto(w, http.StatusOK, &cloudpb.LogoutAccountSessionResponse{}, err)
	})
	mux.HandleFunc("POST /api/v1/account/password", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.ChangeAccountPasswordRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ChangeAccountPasswordResponse
		if err == nil {
			response, err = config.Commerce.ChangePassword(r.Context(), account.GetAccountId(), request)
		}
		if err == nil {
			err = setProductSession(w, response.GetSession(), config.SecureCookie)
		}
		writeProductProto(w, http.StatusOK, sanitizePasswordResponse(response), err)
	})
	mux.HandleFunc("GET /api/v1/account/commerce", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		var response *cloudpb.GetAccountCommerceResponse
		if err == nil {
			response, err = config.Commerce.AccountCommerce(r.Context(), account.GetAccountId())
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/checkout", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.CreateCheckoutRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateCheckoutResponse
		if err == nil {
			response, err = config.Commerce.CreateCheckout(r.Context(), account.GetAccountId(), account.GetUserId(), request)
		}
		if err == nil && config.CheckoutProvider != nil {
			var attempt *cloudpb.PaymentAttemptProjection
			var checkoutURL string
			attempt, checkoutURL, err = config.CheckoutProvider.StartCheckout(r.Context(), account, response.GetOrder())
			if err == nil {
				response.PaymentAttempt = attempt
				response.Provider = "creem"
				response.CheckoutUrl = checkoutURL
			}
		}
		writeProductProto(w, http.StatusCreated, response, err)
	})
	if config.EnableTestPaymentProvider {
		mux.HandleFunc("POST /api/v1/checkout/test-payment", func(w http.ResponseWriter, r *http.Request) {
			account, _, err := authorizeProductMutation(r, config.Commerce)
			request := &cloudpb.ConfirmTestPaymentRequest{}
			if err == nil {
				err = decodeProductProto(r, request)
			}
			var response *cloudpb.ConfirmTestPaymentResponse
			if err == nil {
				response, err = config.Commerce.ConfirmTestPayment(r.Context(), account.GetAccountId(), request)
			}
			writeProductProto(w, http.StatusOK, response, err)
		})
	}
	mux.HandleFunc("POST /api/v1/subscription/transition", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.TransitionSubscriptionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.TransitionSubscriptionResponse
		if err == nil {
			if request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_CANCEL_AT_PERIOD_END && request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESUME {
				err = commerce.ErrUnauthorized
			}
		}
		if err == nil {
			request.AccountId = account.GetAccountId()
			request.ActorId = account.GetUserId()
			response, err = config.Commerce.Transition(r.Context(), request)
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}

func authenticateProduct(r *http.Request, service *commerce.Service) (*cloudpb.AccountProjection, string, error) {
	cookie, err := r.Cookie(productAccessCookie)
	if err != nil {
		return nil, "", commerce.ErrUnauthorized
	}
	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil, "", commerce.ErrUnauthorized
	}
	return service.AuthenticateAccess(r.Context(), token)
}

func authorizeProductMutation(r *http.Request, service *commerce.Service) (*cloudpb.AccountProjection, string, error) {
	if !productMutationAllowed(r) {
		return nil, "", commerce.ErrUnauthorized
	}
	return authenticateProduct(r, service)
}

func productMutationAllowed(r *http.Request) bool {
	cookie, err := r.Cookie(productCSRFCookie)
	return err == nil && sameOrigin(r) && cookie.Value != "" && cookie.Value == r.Header.Get("X-Muxvia-CSRF")
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

func setProductSession(w http.ResponseWriter, credential *cloudpb.AccountSessionCredential, secure bool) error {
	if credential == nil || len(credential.GetAccessToken()) == 0 || len(credential.GetRefreshToken()) == 0 {
		return commerce.ErrUnauthorized
	}
	csrf := make([]byte, 24)
	if _, err := rand.Read(csrf); err != nil {
		return err
	}
	accessAge := max(1, int(time.Until(time.UnixMilli(credential.GetAccessExpiresAtUnixMillis())).Seconds()))
	refreshAge := max(1, int(time.Until(time.UnixMilli(credential.GetRefreshExpiresAtUnixMillis())).Seconds()))
	http.SetCookie(w, &http.Cookie{Name: productAccessCookie, Value: base64.RawURLEncoding.EncodeToString(credential.GetAccessToken()), Path: "/", MaxAge: accessAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: productRefreshCookie, Value: base64.RawURLEncoding.EncodeToString(credential.GetRefreshToken()), Path: "/api/v1/account", MaxAge: refreshAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: productCSRFCookie, Value: hex.EncodeToString(csrf), Path: "/", MaxAge: refreshAge, Secure: secure, SameSite: http.SameSiteStrictMode})
	return nil
}

func clearProductSession(w http.ResponseWriter, secure bool) {
	for _, cookie := range []http.Cookie{{Name: productAccessCookie, Path: "/", HttpOnly: true}, {Name: productRefreshCookie, Path: "/api/v1/account", HttpOnly: true}, {Name: productCSRFCookie, Path: "/"}} {
		cookie.MaxAge = -1
		cookie.Secure = secure
		cookie.SameSite = http.SameSiteStrictMode
		http.SetCookie(w, &cookie)
	}
}

func decodeProductProto(r *http.Request, target proto.Message) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return err
	}
	return protojson.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(body, target)
}

func writeProductProto(w http.ResponseWriter, status int, value proto.Message, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		if errors.Is(err, commerce.ErrUnauthorized) {
			status = http.StatusUnauthorized
		} else if errors.Is(err, commerce.ErrNotFound) {
			status = http.StatusNotFound
		} else if status < 400 {
			status = http.StatusConflict
		}
		writeProductError(w, status, err)
		return
	}
	w.WriteHeader(status)
	if value != nil {
		body, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value)
		_, _ = w.Write(body)
	}
}

func writeProductError(w http.ResponseWriter, status int, err error) {
	code := "conflict"
	message := "cloud product request conflicts with current state"
	if errors.Is(err, commerce.ErrUnauthorized) {
		code = "unauthorized"
		message = "account credential is invalid"
	} else if errors.Is(err, commerce.ErrNotFound) {
		code = "not_found"
		message = "cloud product resource was not found"
	}
	body, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&cloudpb.CloudProductError{Code: code, Message: message, Retryable: false})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func sanitizeCredential(credential *cloudpb.AccountSessionCredential) *cloudpb.AccountSessionCredential {
	if credential == nil {
		return nil
	}
	value := proto.Clone(credential).(*cloudpb.AccountSessionCredential)
	value.AccessToken = nil
	value.RefreshToken = nil
	return value
}

func sanitizeCredentialResponse(response *cloudpb.RegisterAccountResponse) *cloudpb.RegisterAccountResponse {
	if response == nil {
		return nil
	}
	return &cloudpb.RegisterAccountResponse{Session: sanitizeCredential(response.GetSession())}
}

func sanitizeLoginResponse(response *cloudpb.PasswordLoginResponse) *cloudpb.PasswordLoginResponse {
	if response == nil {
		return nil
	}
	return &cloudpb.PasswordLoginResponse{Session: sanitizeCredential(response.GetSession())}
}

func sanitizeRefreshResponse(response *cloudpb.RefreshAccountSessionResponse) *cloudpb.RefreshAccountSessionResponse {
	if response == nil {
		return nil
	}
	return &cloudpb.RefreshAccountSessionResponse{Session: sanitizeCredential(response.GetSession())}
}

func sanitizePasswordResponse(response *cloudpb.ChangeAccountPasswordResponse) *cloudpb.ChangeAccountPasswordResponse {
	if response == nil {
		return nil
	}
	return &cloudpb.ChangeAccountPasswordResponse{Session: sanitizeCredential(response.GetSession())}
}

func productCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if strings.EqualFold(cookie.Name, name) {
			return cookie
		}
	}
	return nil
}
