package apihttp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	immutableCacheControl = "public, max-age=31536000, immutable"
	shortCacheControl     = "public, max-age=300"
)

func TestEmbeddedWebBuildUsesManifestAssetsForImmutableCaching(t *testing.T) {
	handler := newStaticHandler(t, webFiles)
	index := requestStatic(t, handler, "/index.html")
	if index.Code != http.StatusOK || index.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index status=%d cache=%q", index.Code, index.Header().Get("Cache-Control"))
	}

	references := regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(references) == 0 {
		t.Fatal("built index does not reference any assets")
	}
	for _, reference := range references {
		assetURL := reference[1]
		if _, ok := handler.immutableAssetPaths[assetURL]; !ok {
			t.Fatalf("index asset URL is absent from Vite manifest: %q", assetURL)
		}
		assertImmutableAsset(t, handler, assetURL)
	}

	err := fs.WalkDir(webFiles, "web/assets", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		assetURL := "/" + strings.TrimPrefix(name, "web/")
		if _, ok := handler.immutableAssetPaths[assetURL]; !ok {
			t.Fatalf("built static file is absent from Vite manifest: %q", assetURL)
		}
		assertImmutableAsset(t, handler, assetURL)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStaticCachePolicyUsesExactManifestMembership(t *testing.T) {
	const generatedAssetURL = "/assets/index-D_4u8aFf.js"
	staticFiles := fstest.MapFS{
		"web/index.html":                  {Data: []byte(`<script src="` + generatedAssetURL + `"></script>`)},
		"web/assets/index-D_4u8aFf.js":    {Data: []byte("generated")},
		"web/assets/release-candidate.js": {Data: []byte("fixed words")},
		"web/assets/runtime-20260731.js":  {Data: []byte("date")},
		"web/assets/runtime-abcdefgh.js":  {Data: []byte("fake eight-character suffix")},
	}
	handler := &handler{
		staticFiles: staticFiles,
		immutableAssetPaths: map[string]struct{}{
			generatedAssetURL: {},
		},
	}

	if cache := requestStatic(t, handler, "/app/devices").Header().Get("Cache-Control"); cache != "no-cache" {
		t.Fatalf("deep-link HTML cache=%q", cache)
	}
	assertImmutableAsset(t, handler, generatedAssetURL)
	for _, assetURL := range []string{
		"/assets/release-candidate.js",
		"/assets/runtime-20260731.js",
		"/assets/runtime-abcdefgh.js",
	} {
		assertShortLivedAsset(t, handler, assetURL)
	}
}

func TestViteAssetManifestIsNotHTTPAccessible(t *testing.T) {
	response := requestStatic(t, newStaticHandler(t, webFiles), "/"+viteAssetManifestName)
	if response.Code != http.StatusNotFound {
		t.Fatalf("manifest status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestViteAssetManifestFailsClosed(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"missing": {
			"web/index.html": {Data: []byte("index")},
		},
		"invalid JSON": {
			"web/index.html":               {Data: []byte("index")},
			"web/" + viteAssetManifestName: {Data: []byte("{")},
		},
		"empty": {
			"web/index.html":               {Data: []byte("index")},
			"web/" + viteAssetManifestName: {Data: []byte("{}")},
		},
		"entry missing": {
			"web/index.html":               {Data: []byte("index")},
			"web/assets/chunk-realhash.js": {Data: []byte("chunk")},
			"web/" + viteAssetManifestName: {Data: []byte(`{"chunk":{"file":"assets/chunk-realhash.js"}}`)},
		},
		"declared asset missing": {
			"web/index.html":               {Data: []byte("index")},
			"web/" + viteAssetManifestName: {Data: []byte(`{"index.html":{"file":"assets/index-realhash.js","isEntry":true}}`)},
		},
		"generated asset unlisted": {
			"web/index.html":                         {Data: []byte("index")},
			"web/assets/index-realhash.js":           {Data: []byte("index")},
			"web/assets/runtime-releasecandidate.js": {Data: []byte("unlisted")},
			"web/" + viteAssetManifestName:           {Data: []byte(`{"index.html":{"file":"assets/index-realhash.js","isEntry":true}}`)},
		},
	}

	for name, staticFiles := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadViteAssetPaths(staticFiles); err == nil {
				t.Fatal("loadViteAssetPaths succeeded with a missing or damaged manifest")
			}
		})
	}
}

func TestStaticCachePolicyFailsClosedWithoutManifestIndex(t *testing.T) {
	handler := &handler{staticFiles: fstest.MapFS{
		"web/index.html":               {Data: []byte("index")},
		"web/assets/index-realhash.js": {Data: []byte("asset")},
	}}
	response := requestStatic(t, handler, "/assets/index-realhash.js")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestNewHTMLAndHashedChunkRecoverAcrossCachedBuilds(t *testing.T) {
	oldURL, oldBuild := buildFixture(t, "old build")
	newURL, newBuild := buildFixture(t, "new build")
	oldHandler := newStaticHandler(t, oldBuild)
	newHandler := newStaticHandler(t, newBuild)

	oldIndex := requestStatic(t, oldHandler, "/app/devices")
	if !strings.Contains(oldIndex.Body.String(), oldURL) {
		t.Fatalf("old HTML does not reference %q: %s", oldURL, oldIndex.Body.String())
	}
	assertImmutableAsset(t, oldHandler, oldURL)

	newIndex := requestStatic(t, newHandler, "/app/devices")
	if oldURL == newURL || !strings.Contains(newIndex.Body.String(), newURL) || strings.Contains(newIndex.Body.String(), oldURL) {
		t.Fatalf("new HTML did not move from %q to %q: %s", oldURL, newURL, newIndex.Body.String())
	}
	if stale := requestStatic(t, newHandler, oldURL); stale.Code != http.StatusNotFound {
		t.Fatalf("old chunk on new deployment status=%d", stale.Code)
	}
	reloadedIndex := requestStatic(t, newHandler, "/app/devices")
	if reloadedIndex.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(reloadedIndex.Body.String(), newURL) {
		t.Fatalf("reload cache=%q body=%s", reloadedIndex.Header().Get("Cache-Control"), reloadedIndex.Body.String())
	}
	assertImmutableAsset(t, newHandler, newURL)
}

func buildFixture(t *testing.T, payload string) (string, fstest.MapFS) {
	t.Helper()
	digest := sha256.Sum256([]byte(payload))
	assetURL := fmt.Sprintf("/assets/index-%x.js", digest[:4])
	manifest, err := json.Marshal(map[string]any{
		"index.html": map[string]any{"file": strings.TrimPrefix(assetURL, "/"), "isEntry": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return assetURL, fstest.MapFS{
		"web/index.html": {Data: []byte(`<script src="` + assetURL + `"></script>`)},
		"web/" + strings.TrimPrefix(assetURL, "/"): {Data: []byte(payload)},
		"web/" + viteAssetManifestName:             {Data: manifest},
	}
}

func newStaticHandler(t *testing.T, staticFiles fs.FS) *handler {
	t.Helper()
	assetPaths, err := loadViteAssetPaths(staticFiles)
	if err != nil {
		t.Fatal(err)
	}
	return &handler{staticFiles: staticFiles, immutableAssetPaths: assetPaths}
}

func assertImmutableAsset(t *testing.T, handler *handler, assetURL string) {
	t.Helper()
	response := requestStatic(t, handler, assetURL)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != immutableCacheControl {
		t.Fatalf("asset %q status=%d cache=%q", assetURL, response.Code, response.Header().Get("Cache-Control"))
	}
}

func assertShortLivedAsset(t *testing.T, handler *handler, assetURL string) {
	t.Helper()
	response := requestStatic(t, handler, assetURL)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != shortCacheControl {
		t.Fatalf("asset %q status=%d cache=%q", assetURL, response.Code, response.Header().Get("Cache-Control"))
	}
}

func requestStatic(t *testing.T, handler *handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.serveStatic(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}
