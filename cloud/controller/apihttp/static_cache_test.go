package apihttp

import (
	"crypto/sha256"
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

func TestEmbeddedWebBuildUsesHashedImmutableAssets(t *testing.T) {
	handler := &handler{staticFiles: webFiles}
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
		if !contentHashedAssetPath.MatchString(assetURL) {
			t.Fatalf("index asset URL is not content hashed: %q", assetURL)
		}
		assertImmutableAsset(t, handler, assetURL)
	}

	err := fs.WalkDir(webFiles, "web", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || name == "web/index.html" {
			return err
		}
		assetURL := "/" + strings.TrimPrefix(name, "web/")
		if !contentHashedAssetPath.MatchString(assetURL) {
			t.Fatalf("built static file is not content hashed: %q", assetURL)
		}
		assertImmutableAsset(t, handler, assetURL)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStaticCachePolicyKeepsUnhashedAssetsShortLived(t *testing.T) {
	handler := &handler{staticFiles: fstest.MapFS{
		"web/index.html":               {Data: []byte(`<script src="/assets/index-abcdefgh.js"></script>`)},
		"web/assets/index-abcdefgh.js": {Data: []byte("hashed")},
		"web/assets/runtime.js":        {Data: []byte("unhashed")},
	}}

	if cache := requestStatic(t, handler, "/app/devices").Header().Get("Cache-Control"); cache != "no-cache" {
		t.Fatalf("deep-link HTML cache=%q", cache)
	}
	assertImmutableAsset(t, handler, "/assets/index-abcdefgh.js")
	if cache := requestStatic(t, handler, "/assets/runtime.js").Header().Get("Cache-Control"); cache != shortCacheControl {
		t.Fatalf("unhashed asset cache=%q", cache)
	}
}

func TestNewHTMLAndHashedChunkRecoverAcrossCachedBuilds(t *testing.T) {
	oldURL, oldBuild := buildFixture("old build")
	newURL, newBuild := buildFixture("new build")
	oldHandler := &handler{staticFiles: oldBuild}
	newHandler := &handler{staticFiles: newBuild}

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

func buildFixture(payload string) (string, fstest.MapFS) {
	digest := sha256.Sum256([]byte(payload))
	assetURL := fmt.Sprintf("/assets/index-%x.js", digest[:4])
	return assetURL, fstest.MapFS{
		"web/index.html": {Data: []byte(`<script src="` + assetURL + `"></script>`)},
		"web/" + strings.TrimPrefix(assetURL, "/"): {Data: []byte(payload)},
	}
}

func assertImmutableAsset(t *testing.T, handler *handler, assetURL string) {
	t.Helper()
	response := requestStatic(t, handler, assetURL)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != immutableCacheControl {
		t.Fatalf("asset %q status=%d cache=%q", assetURL, response.Code, response.Header().Get("Cache-Control"))
	}
}

func requestStatic(t *testing.T, handler *handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.serveStatic(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}
