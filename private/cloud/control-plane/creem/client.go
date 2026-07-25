// Package creem 实现 Muxvia Commerce 到 Creem REST/Webhook 的唯一正式 provider adapter。
//
// 本包只拥有 provider 协议、签名校验和 normalized event 映射；Order、Subscription、
// Entitlement 与幂等 journal 仍由 commerce/PostgreSQL 拥有。
package creem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ProviderName 是 payment attempt 和 normalized journal 使用的稳定 provider 标识。
	ProviderName      = "creem"
	testBaseURL       = "https://test-api.creem.io"
	productionBaseURL = "https://api.creem.io"
	maxResponseBody   = 1 << 20
)

// Environment 选择 Creem test 或 production API；两种 key 不允许交叉使用。
type Environment string

const (
	// EnvironmentTest 使用 Creem Test Mode，不产生正式收费。
	EnvironmentTest Environment = "test"
	// EnvironmentProduction 使用 Creem production API。
	EnvironmentProduction Environment = "production"
)

var (
	// ErrInvalid 表示 provider 配置或响应违反 Creem/Muxvia 契约。
	ErrInvalid = errors.New("invalid Creem provider data")
	// ErrUnavailable 表示 provider 暂时不可用，调用方只能重试，不能改变 entitlement。
	ErrUnavailable = errors.New("Creem provider unavailable")
)

// APIError 是不包含 provider body 或 secret 的稳定 HTTP 失败。
type APIError struct {
	StatusCode int
	TraceID    string
	Retryable  bool
}

func (err *APIError) Error() string {
	if err.TraceID == "" {
		return fmt.Sprintf("Creem API returned status %d", err.StatusCode)
	}
	return fmt.Sprintf("Creem API returned status %d (trace %s)", err.StatusCode, err.TraceID)
}

func (err *APIError) Unwrap() error {
	if err.Retryable {
		return ErrUnavailable
	}
	return ErrInvalid
}

// HTTPClient 是 Creem REST 调用需要的最小 host port。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// ClientConfig 固定环境、API key、HTTP transport 与超时。
type ClientConfig struct {
	Environment Environment
	APIKey      string
	HTTPClient  HTTPClient
}

// Client 是无业务状态的 Creem REST adapter。
type Client struct {
	baseURL string
	apiKey  string
	http    HTTPClient
}

// NewClient 创建只访问官方 Creem test/production origin 的 adapter。
func NewClient(config ClientConfig) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("%w: API key is required", ErrInvalid)
	}
	baseURL := testBaseURL
	switch config.Environment {
	case EnvironmentTest:
	case EnvironmentProduction:
		baseURL = productionBaseURL
	default:
		return nil, fmt.Errorf("%w: environment is required", ErrInvalid)
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, apiKey: config.APIKey, http: config.HTTPClient}, nil
}

// Checkout 是 create/retrieve checkout 的 provider projection；不直接成为产品真值。
type Checkout struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"`
	Product      Reference         `json:"product"`
	RequestID    string            `json:"request_id"`
	Order        *Order            `json:"order"`
	Subscription Reference         `json:"subscription"`
	CheckoutURL  string            `json:"checkout_url"`
	Metadata     map[string]string `json:"metadata"`
	Discount     *Discount         `json:"discount"`
}

// Order 是 Creem checkout 内的经济投影。
type Order struct {
	ID             string    `json:"id"`
	Product        Reference `json:"product"`
	Transaction    Reference `json:"transaction"`
	Discount       Reference `json:"discount"`
	Amount         int64     `json:"amount"`
	Subtotal       int64     `json:"sub_total"`
	DiscountAmount int64     `json:"discount_amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
}

// Transaction 是 reconciliation 核对的服务端支付事实。
type Transaction struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	Amount         int64     `json:"amount"`
	DiscountAmount int64     `json:"discount_amount"`
	Currency       string    `json:"currency"`
	Order          Reference `json:"order"`
	Subscription   Reference `json:"subscription"`
	CreatedAt      int64     `json:"created_at"`
}

// Subscription 是定期 reconciliation 使用的 Creem 订阅投影。
// Muxvia 只读取 provider 状态，不把该对象直接作为 Entitlement 真值。
type Subscription struct {
	ID                string            `json:"id"`
	Status            string            `json:"status"`
	Product           Reference         `json:"product"`
	LastTransactionID string            `json:"last_transaction_id"`
	UpdatedAt         string            `json:"updated_at"`
	Metadata          map[string]string `json:"metadata"`
}

// Product 是 catalog mapping 核对所需的 Creem 商品字段。
type Product struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Price         int64  `json:"price"`
	Currency      string `json:"currency"`
	BillingType   string `json:"billing_type"`
	BillingPeriod string `json:"billing_period"`
}

// Discount 是 promotion mapping 核对所需的 Creem 优惠字段。
type Discount struct {
	ID                string   `json:"id"`
	Status            string   `json:"status"`
	Code              string   `json:"code"`
	Type              string   `json:"type"`
	Amount            int64    `json:"amount"`
	Currency          string   `json:"currency"`
	Percentage        float64  `json:"percentage"`
	ExpiryDate        string   `json:"expiry_date"`
	MaxRedemptions    uint32   `json:"max_redemptions"`
	AppliesToProducts []string `json:"applies_to_products"`
}

// Reference 接受 Creem API 在不同 endpoint 中返回的 string 或内嵌 object reference。
type Reference string

func (reference *Reference) UnmarshalJSON(body []byte) error {
	if bytes.Equal(body, []byte("null")) {
		*reference = ""
		return nil
	}
	var value string
	if json.Unmarshal(body, &value) == nil {
		*reference = Reference(value)
		return nil
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &object); err != nil || object.ID == "" {
		return ErrInvalid
	}
	*reference = Reference(object.ID)
	return nil
}

type createCheckoutRequest struct {
	ProductID    string            `json:"product_id"`
	RequestID    string            `json:"request_id"`
	Units        uint32            `json:"units"`
	DiscountCode string            `json:"discount_code,omitempty"`
	Customer     checkoutCustomer  `json:"customer"`
	SuccessURL   string            `json:"success_url"`
	Metadata     map[string]string `json:"metadata"`
}

type checkoutCustomer struct {
	Email string `json:"email"`
}

// CreateCheckout 调用 Creem checkout API；request_id 必须使用稳定 Muxvia order ID。
func (client *Client) CreateCheckout(ctx context.Context, productID, requestID, email, discountCode, successURL string, metadata map[string]string) (*Checkout, error) {
	if productID == "" || requestID == "" || email == "" || successURL == "" {
		return nil, ErrInvalid
	}
	request := createCheckoutRequest{ProductID: productID, RequestID: requestID, Units: 1, DiscountCode: discountCode, Customer: checkoutCustomer{Email: email}, SuccessURL: successURL, Metadata: metadata}
	result := &Checkout{}
	if err := client.request(ctx, http.MethodPost, "/v1/checkouts", nil, request, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Checkout 返回一个 Creem checkout 的当前服务端状态。
func (client *Client) Checkout(ctx context.Context, checkoutID string) (*Checkout, error) {
	result := &Checkout{}
	if err := client.request(ctx, http.MethodGet, "/v1/checkouts", url.Values{"checkout_id": {checkoutID}}, nil, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Transaction 返回一个 Creem transaction 的当前服务端状态。
func (client *Client) Transaction(ctx context.Context, transactionID string) (*Transaction, error) {
	result := &Transaction{}
	if err := client.request(ctx, http.MethodGet, "/v1/transactions", url.Values{"transaction_id": {transactionID}}, nil, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Subscription 返回一个 Creem subscription 的当前服务端状态。
func (client *Client) Subscription(ctx context.Context, subscriptionID string) (*Subscription, error) {
	result := &Subscription{}
	if err := client.request(ctx, http.MethodGet, "/v1/subscriptions", url.Values{"subscription_id": {subscriptionID}}, nil, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Product 返回一个已在 Creem 管理的商品，用于核对 catalog mapping。
func (client *Client) Product(ctx context.Context, productID string) (*Product, error) {
	result := &Product{}
	if err := client.request(ctx, http.MethodGet, "/v1/products", url.Values{"product_id": {productID}}, nil, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Discount 返回一个已在 Creem 管理的优惠，用于 operator 登记 mapping 前核对。
func (client *Client) Discount(ctx context.Context, code string) (*Discount, error) {
	result := &Discount{}
	if err := client.request(ctx, http.MethodGet, "/v1/discounts", url.Values{"discount_code": {code}}, nil, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (client *Client) request(ctx context.Context, method, path string, query url.Values, input, output any) error {
	if client == nil || client.apiKey == "" || path == "" {
		return ErrInvalid
	}
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return ErrInvalid
		}
		body = bytes.NewReader(payload)
	}
	endpoint := client.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("x-api-key", client.apiKey)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil || len(payload) > maxResponseBody {
		return ErrUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var providerError struct {
			TraceID string `json:"trace_id"`
		}
		_ = json.Unmarshal(payload, &providerError)
		return &APIError{StatusCode: response.StatusCode, TraceID: providerError.TraceID, Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}
	}
	if output == nil || json.Unmarshal(payload, output) != nil {
		return ErrInvalid
	}
	return nil
}
