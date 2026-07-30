package apihttp

import (
	"encoding/json"
	"fmt"
	"io/fs"
	pathpkg "path"
	"strings"
)

const viteAssetManifestName = "asset-manifest.json"

type viteManifestEntry struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
	IsEntry bool     `json:"isEntry"`
}

func loadViteAssetPaths(staticFiles fs.FS) (map[string]struct{}, error) {
	payload, err := fs.ReadFile(staticFiles, "web/"+viteAssetManifestName)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", viteAssetManifestName, err)
	}

	manifest := map[string]viteManifestEntry{}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", viteAssetManifestName, err)
	}
	if len(manifest) == 0 {
		return nil, fmt.Errorf("decode %s: manifest is empty", viteAssetManifestName)
	}

	assetPaths := make(map[string]struct{})
	hasEntry := false
	for source, entry := range manifest {
		if strings.TrimSpace(source) == "" {
			return nil, fmt.Errorf("decode %s: manifest source is empty", viteAssetManifestName)
		}
		if entry.IsEntry {
			hasEntry = true
		}
		if err := addViteAssetPath(staticFiles, assetPaths, source, entry.File); err != nil {
			return nil, err
		}
		for _, name := range entry.CSS {
			if err := addViteAssetPath(staticFiles, assetPaths, source, name); err != nil {
				return nil, err
			}
		}
		for _, name := range entry.Assets {
			if err := addViteAssetPath(staticFiles, assetPaths, source, name); err != nil {
				return nil, err
			}
		}
	}
	if !hasEntry {
		return nil, fmt.Errorf("decode %s: entry chunk is missing", viteAssetManifestName)
	}

	if err := fs.WalkDir(staticFiles, "web/assets", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		assetPath := "/" + strings.TrimPrefix(name, "web/")
		if _, ok := assetPaths[assetPath]; !ok {
			return fmt.Errorf("validate %s: generated asset %q is not listed", viteAssetManifestName, assetPath)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return assetPaths, nil
}

func addViteAssetPath(staticFiles fs.FS, assetPaths map[string]struct{}, source, name string) error {
	if name == "" {
		return fmt.Errorf("decode %s: manifest source %q has an empty asset path", viteAssetManifestName, source)
	}
	if pathpkg.Clean(name) != name || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || !strings.HasPrefix(name, "assets/") {
		return fmt.Errorf("decode %s: manifest source %q has invalid asset path %q", viteAssetManifestName, source, name)
	}
	info, err := fs.Stat(staticFiles, "web/"+name)
	if err != nil {
		return fmt.Errorf("validate %s asset %q: %w", viteAssetManifestName, name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("validate %s asset %q: not a regular file", viteAssetManifestName, name)
	}
	assetPaths["/"+name] = struct{}{}
	return nil
}
