package webcontroller

import (
	"context"
	"net/http"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// DaemonEnrollmentService 是 Web 账号 surface 到 Controller enrollment 状态机的唯一边界。
type DaemonEnrollmentService interface {
	CreateDaemonEnrollment(context.Context, string, string) (*cloudpb.DaemonEnrollmentProjection, error)
	InspectDaemonEnrollment(context.Context, string, string) (*cloudpb.DaemonEnrollmentProjection, error)
	ApproveDaemonEnrollment(context.Context, string, string) (*cloudpb.ApproveDaemonEnrollmentResponse, error)
}

// DaemonEnrollmentAPIConfig 固定账号认证与 Controller 内存 flow owner。
type DaemonEnrollmentAPIConfig struct {
	Commerce *commerce.Service
	Service  DaemonEnrollmentService
}

// DaemonEnrollmentAPIHandler 暴露已认证 Web 创建、查询和批准 daemon 的 Proto JSON API。
func DaemonEnrollmentAPIHandler(config DaemonEnrollmentAPIConfig) (http.Handler, error) {
	if config.Commerce == nil || config.Service == nil {
		return nil, commerce.ErrConflict
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/daemon-enrollments/create", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.CreateDaemonEnrollmentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.DaemonEnrollmentProjection
		if err == nil {
			response, err = config.Service.CreateDaemonEnrollment(r.Context(), account.GetAccountId(), account.GetUserId())
		}
		writeProductProto(w, http.StatusCreated, response, err)
	})
	mux.HandleFunc("POST /api/v1/daemon-enrollments/inspect", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		request := &cloudpb.InspectDaemonEnrollmentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.DaemonEnrollmentProjection
		if err == nil {
			response, err = config.Service.InspectDaemonEnrollment(r.Context(), account.GetAccountId(), request.GetUserCode())
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/daemon-enrollments/approve", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.ApproveDaemonEnrollmentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ApproveDaemonEnrollmentResponse
		if err == nil {
			response, err = config.Service.ApproveDaemonEnrollment(r.Context(), account.GetAccountId(), request.GetUserCode())
		}
		writeProductProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}
