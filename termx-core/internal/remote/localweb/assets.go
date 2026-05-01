package localweb

import (
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

type staticAssets struct {
	files map[string]string
}

type staticFile struct {
	name string
	data string
}

type staticFileInfo struct {
	name string
	size int64
}

func NewStaticAssets(files map[string]string) fs.FS {
	copied := make(map[string]string, len(files))
	for name, data := range files {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		copied[name] = data
	}
	return staticAssets{files: copied}
}

func (a staticAssets) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name == "." {
		name = "index.html"
	}
	if data, ok := a.files[name]; ok {
		return &staticFile{name: name, data: data}, nil
	}
	prefix := strings.TrimSuffix(name, "/") + "/"
	for fileName := range a.files {
		if strings.HasPrefix(fileName, prefix) {
			return staticDir{name: strings.TrimSuffix(name, "/"), entries: a.dirEntries(prefix)}, nil
		}
	}
	return nil, fs.ErrNotExist
}

func (a staticAssets) dirEntries(prefix string) []fs.DirEntry {
	seen := make(map[string]struct{})
	for fileName := range a.files {
		if !strings.HasPrefix(fileName, prefix) {
			continue
		}
		rest := strings.TrimPrefix(fileName, prefix)
		part := rest
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			part = rest[:idx]
		}
		if part != "" {
			seen[part] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, staticDirEntry{name: name})
	}
	return entries
}

func (f *staticFile) Stat() (fs.FileInfo, error) {
	return staticFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *staticFile) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, io.EOF
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func (f *staticFile) Close() error { return nil }

func (i staticFileInfo) Name() string       { return i.name }
func (i staticFileInfo) Size() int64        { return i.size }
func (i staticFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i staticFileInfo) ModTime() time.Time { return time.Time{} }
func (i staticFileInfo) IsDir() bool        { return false }
func (i staticFileInfo) Sys() any           { return nil }

type staticDir struct {
	name    string
	entries []fs.DirEntry
	offset  int
}

func (d staticDir) Stat() (fs.FileInfo, error) {
	return staticDirInfo{name: d.name}, nil
}

func (d staticDir) Read([]byte) (int, error) {
	return 0, fs.ErrInvalid
}

func (d staticDir) Close() error { return nil }

func (d *staticDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.offset >= len(d.entries) {
		return nil, io.EOF
	}
	if n <= 0 || d.offset+n > len(d.entries) {
		n = len(d.entries) - d.offset
	}
	out := d.entries[d.offset : d.offset+n]
	d.offset += n
	return out, nil
}

type staticDirInfo struct {
	name string
}

func (i staticDirInfo) Name() string       { return i.name }
func (i staticDirInfo) Size() int64        { return 0 }
func (i staticDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (i staticDirInfo) ModTime() time.Time { return time.Time{} }
func (i staticDirInfo) IsDir() bool        { return true }
func (i staticDirInfo) Sys() any           { return nil }

type staticDirEntry struct {
	name string
}

func (e staticDirEntry) Name() string               { return e.name }
func (e staticDirEntry) IsDir() bool                { return false }
func (e staticDirEntry) Type() fs.FileMode          { return 0 }
func (e staticDirEntry) Info() (fs.FileInfo, error) { return staticFileInfo{name: e.name}, nil }
