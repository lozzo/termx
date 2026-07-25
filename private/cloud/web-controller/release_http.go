package webcontroller

import (
	"net/http"

	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// ReleaseAPIHandler 暴露账号无关的签名发布解析；客户端只提交版本与稳定分桶身份。
func ReleaseAPIHandler(service *releasecatalog.Service) (http.Handler, error) {
	if service == nil {
		return nil, releasecatalog.ErrInvalid
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/releases/resolve", func(w http.ResponseWriter, r *http.Request) {
		request := &cloudpb.ResolveClientReleaseRequest{}
		err := decodeProductProto(r, request)
		var response *cloudpb.ResolveClientReleaseResponse
		if err == nil {
			response, err = service.Resolve(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}
