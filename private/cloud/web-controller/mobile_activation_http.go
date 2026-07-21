package webcontroller

import (
	"context"
	"net/http"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// MobileActivationService 是 Web 账号 surface 到 Controller 扫码登录状态机的唯一边界。
type MobileActivationService interface {
	CreateMobileActivation(context.Context, string, string) (*cloudpb.MobileActivationProjection, error)
	InspectMobileActivation(context.Context, string, string) (*cloudpb.MobileActivationProjection, error)
	ApproveMobileActivation(context.Context, string, string) (*cloudpb.MobileActivationApproveResponse, error)
}

// MobileActivationAPIConfig 固定账号认证与 Controller 扫码登录 owner。
type MobileActivationAPIConfig struct {
	Commerce *commerce.Service
	Service  MobileActivationService
}

// MobileActivationAPIHandler 暴露已认证 Web 创建、查询和批准二维码的 Proto JSON API。
func MobileActivationAPIHandler(config MobileActivationAPIConfig) (http.Handler, error) {
	if config.Commerce == nil || config.Service == nil {
		return nil, commerce.ErrConflict
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/mobile-activations/create", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.MobileActivationCreateRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.MobileActivationProjection
		if err == nil {
			response, err = config.Service.CreateMobileActivation(r.Context(), account.GetAccountId(), account.GetUserId())
		}
		writeProductProto(w, http.StatusCreated, response, err)
	})
	mux.HandleFunc("POST /api/v1/mobile-activations/inspect", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		request := &cloudpb.MobileActivationInspectRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.MobileActivationProjection
		if err == nil {
			response, err = config.Service.InspectMobileActivation(r.Context(), account.GetAccountId(), request.GetUserCode())
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/mobile-activations/approve", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.MobileActivationApproveRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.MobileActivationApproveResponse
		if err == nil {
			response, err = config.Service.ApproveMobileActivation(r.Context(), account.GetAccountId(), request.GetUserCode())
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}
