package webcontroller

import (
	"encoding/json"
	"io"
	"net/http"
)

// IdentityProvider 描述登录页可用的外部身份提供商；Configured=false 时浏览器必须禁用入口。
type IdentityProvider struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Configured       bool   `json:"configured"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
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
		if err == ErrCommerceUnauthorized && status < 400 {
			status = http.StatusUnauthorized
		} else if status < 400 {
			status = http.StatusConflict
		}
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
		return
	}
	writer.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(writer).Encode(value)
	}
}
