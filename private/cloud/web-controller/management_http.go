package webcontroller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	"github.com/lozzow/termx/private/cloud/control-plane/commerce"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TerminalAccessQuery 是 Web 管理面对 Controller 持久 topology projection 的只读边界。
// 返回值只能是 daemon 上报的 opaque access projection，不能查询 CapabilityGrant body。
type TerminalAccessQuery interface {
	ListTerminalAccess(context.Context, string, string, cloudpb.TerminalAccessState, int) ([]cloudtopology.StoredTerminalAccess, cloudpb.Freshness, time.Time, error)
}

// ManagementAPIConfig 把账号认证与持久 CommandOutbox 接到 Controller public listener。
type ManagementAPIConfig struct {
	Commerce *commerce.Service
	Planner  *commandoutbox.Planner
	Outbox   *commandoutbox.Service
	Accesses TerminalAccessQuery
	Now      func() time.Time
}

// ManagementAPIHandler 提供 generated Proto JSON command 创建和查询。
// Web 不能直连 Hub/daemon；所有 destructive action 先进入 durable CommandOutbox。
func ManagementAPIHandler(config ManagementAPIConfig) (http.Handler, error) {
	if config.Commerce == nil || config.Planner == nil || config.Outbox == nil || config.Accesses == nil {
		return nil, commandoutbox.ErrCommandConflict
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/management/commands", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authorizeProductMutation(r, config.Commerce)
		request := &cloudpb.CreateManagementCommandRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateManagementCommandResponse
		if err == nil {
			request.AccountId = account.GetAccountId()
			command, _, createErr := config.Planner.Create(r.Context(), request, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: account.GetUserId(), DisplayLabel: account.GetDisplayName()}, config.Now().UTC())
			err = createErr
			response = &cloudpb.CreateManagementCommandResponse{Command: command}
		}
		writeManagementProto(w, http.StatusAccepted, response, err)
	})
	mux.HandleFunc("POST /api/v1/management/commands/get", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		request := &cloudpb.GetManagementCommandRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetManagementCommandResponse
		if err == nil {
			command, getErr := config.Outbox.Get(r.Context(), account.GetAccountId(), request.GetCommandId())
			err = getErr
			response = &cloudpb.GetManagementCommandResponse{Command: command}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/management/commands/list", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		request := &cloudpb.ListManagementCommandsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListManagementCommandsResponse
		if err == nil {
			limit := 50
			if requested := request.GetPage().GetPageSize(); requested > 0 && requested <= 100 {
				limit = int(requested)
			}
			commands, listErr := config.Outbox.List(r.Context(), account.GetAccountId(), request.GetCommandKind(), request.GetExecutionState(), limit)
			err = listErr
			response = &cloudpb.ListManagementCommandsResponse{Commands: commands, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/management/terminal-access/list", func(w http.ResponseWriter, r *http.Request) {
		account, _, err := authenticateProduct(r, config.Commerce)
		request := &cloudpb.ListDaemonTerminalAccessRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListDaemonTerminalAccessResponse
		if err == nil {
			limit := 50
			if requested := request.GetPage().GetPageSize(); requested > 0 && requested <= 100 {
				limit = int(requested)
			}
			stored, freshness, observedAt, listErr := config.Accesses.ListTerminalAccess(r.Context(), account.GetAccountId(), request.GetDaemonDeviceId(), request.GetState(), limit)
			err = listErr
			accesses := make([]*cloudpb.TerminalAccessProjection, 0, len(stored))
			for _, item := range stored {
				if item.Value != nil {
					accesses = append(accesses, proto.Clone(item.Value).(*cloudpb.TerminalAccessProjection))
				}
			}
			response = &cloudpb.ListDaemonTerminalAccessResponse{Accesses: accesses, Freshness: freshness, ObservedAtUnixMillis: observedAt.UnixMilli(), Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}

func writeManagementProto(w http.ResponseWriter, status int, value proto.Message, err error) {
	if err == nil {
		writeProductProto(w, status, value, nil)
		return
	}
	detail := &cloudpb.ManagementErrorDetail{Code: cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_TEMPORARY, Message: "management command could not be completed", Retryable: false}
	switch {
	case errors.Is(err, commerce.ErrUnauthorized):
		status = http.StatusUnauthorized
		detail.Code = cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_UNAUTHENTICATED
		detail.Message = "account credential or mutation proof is invalid"
	case errors.Is(err, commandoutbox.ErrCommandNotFound):
		status = http.StatusNotFound
		detail.Code = cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_NOT_FOUND
		detail.Message = "management target or command was not found"
	case errors.Is(err, cloudtopology.ErrOwnershipNotFound):
		status = http.StatusNotFound
		detail.Code = cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_NOT_FOUND
		detail.Message = "management target or command was not found"
	case errors.Is(err, commandoutbox.ErrCommandConflict):
		status = http.StatusConflict
		detail.Code = cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_STALE_TARGET
		detail.Message = "management target conflicts with current authority or topology"
	default:
		status = http.StatusInternalServerError
		detail.Retryable = true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	body, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(detail)
	_, _ = w.Write(body)
}
