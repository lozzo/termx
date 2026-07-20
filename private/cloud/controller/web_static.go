package controller

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// webStaticHandler 从 Controller composition 内提供同一份用户与 operator SPA。
// API 路由由外层 mux 优先匹配；不存在的静态页面回退到 index.html，不会吞掉 API 404。
func webStaticHandler(root string) (http.Handler, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(absolute, "index.html")
	if info, statErr := os.Stat(indexPath); statErr != nil || info.IsDir() {
		return nil, fmt.Errorf("Controller web static index is unavailable: %s", indexPath)
	}
	files := http.FileServer(http.Dir(absolute))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean != "." && !strings.HasPrefix(clean, "..") {
			if info, statErr := os.Stat(filepath.Join(absolute, clean)); statErr == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, indexPath)
	}), nil
}
