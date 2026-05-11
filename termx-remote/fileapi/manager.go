package fileapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type Manager struct {
	mu        sync.Mutex
	transfers map[string]*FileTransfer
	stopCh    chan struct{}
	stopOnce  sync.Once
}

type FileTransfer struct {
	ID        string
	Path      string
	Size      int64
	ChunkSize int
	Direction string
	Offset    int64
	Length    int64
	TempPath  string
	CreatedAt time.Time
}

type FileEntry struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModTime    string `json:"mod_time"`
	LinkTarget string `json:"link_target,omitempty"`
	ChildCount *int   `json:"child_count,omitempty"`
	HardLink   bool   `json:"hard_link,omitempty"`
	LinkCount  uint64 `json:"link_count,omitempty"`
	Inode      uint64 `json:"inode,omitempty"`
}

type DirListResponse struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
	Parent  string      `json:"parent"`
	Total   int         `json:"total"`
}

const (
	chunkSize        = 64 * 1024
	transferTimeout  = 30 * time.Minute
	resumeFileMaxAge = 7 * 24 * time.Hour
	cleanupInterval  = 5 * time.Minute
	frameData        = 0x01
	frameComplete    = 0x02
	frameError       = 0xFF

	downloadSendBufferLimit     = 128 * 1024
	downloadSendBufferLow       = 32 * 1024
	defaultDownloadRateLimitBPS = 0
	downloadRateLimitBPSEnvName = "TERMX_FILE_DOWNLOAD_LIMIT_BPS"
)

func NewManager() *Manager {
	mgr := &Manager{
		transfers: make(map[string]*FileTransfer),
		stopCh:    make(chan struct{}),
	}
	go mgr.cleanupLoop()
	return mgr
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

func (m *Manager) RouteRequest(method, path string, body []byte) (int32, []byte, string) {
	switch {
	case method == http.MethodPost && path == "/files/list":
		return m.handleListDir(body)
	case method == http.MethodPost && path == "/files/stat":
		return m.handleStat(body)
	case method == http.MethodPost && path == "/files/mkdir":
		return m.handleMkdir(body)
	case method == http.MethodPost && path == "/files/delete":
		return m.handleDelete(body)
	case method == http.MethodPost && path == "/files/rename":
		return m.handleRename(body)
	case method == http.MethodPost && path == "/files/copy":
		return m.handleCopy(body)
	case method == http.MethodPost && path == "/files/move":
		return m.handleMove(body)
	case method == http.MethodPost && path == "/files/batch-delete":
		return m.handleBatchDelete(body)
	case method == http.MethodPost && path == "/files/preview":
		return m.handlePreview(body)
	case method == http.MethodPost && path == "/files/download/init":
		return m.handleDownloadInit(body)
	case method == http.MethodPost && path == "/files/upload/init":
		return m.handleUploadInit(body)
	case method == http.MethodPost && path == "/files/upload/complete":
		return m.handleUploadComplete(body)
	default:
		return http.StatusNotFound, nil, fmt.Sprintf("unknown file route: %s %s", method, path)
	}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for id, transfer := range m.transfers {
				if now.Sub(transfer.CreatedAt) > transferTimeout {
					delete(m.transfers, id)
				}
			}
			m.mu.Unlock()
			cleanupOldUploadResumeFiles()
		case <-m.stopCh:
			return
		}
	}
}

func validatePath(p string) (string, error) {
	if p == "" {
		u, err := user.Current()
		if err != nil {
			return "/", nil
		}
		return u.HomeDir, nil
	}

	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute")
	}

	cleaned := filepath.Clean(p)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		resolved = cleaned
	}
	for _, prefix := range []string{"/proc", "/sys", "/dev"} {
		if resolved == prefix || strings.HasPrefix(resolved, prefix+"/") {
			return "", fmt.Errorf("access denied: %s", p)
		}
	}
	return cleaned, nil
}

func (m *Manager) handleListDir(body []byte) (int32, []byte, string) {
	var req struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	_ = json.Unmarshal(body, &req)

	dirPath, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}

	total := len(entries)
	limit := req.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	end := offset + limit
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}

	fileEntries := make([]FileEntry, 0, end-offset)
	for _, entry := range entries[offset:end] {
		fullPath := filepath.Join(dirPath, entry.Name())
		fileEntry, ok := describeFileEntry(fullPath, entry.Name())
		if !ok {
			continue
		}
		fileEntries = append(fileEntries, fileEntry)
	}

	parent := ""
	if dirPath != "/" {
		parent = filepath.Dir(dirPath)
	}
	data, _ := json.Marshal(DirListResponse{
		Path:    dirPath,
		Entries: fileEntries,
		Parent:  parent,
		Total:   total,
	})
	return http.StatusOK, data, ""
}

func (m *Manager) handleStat(body []byte) (int32, []byte, string) {
	var req struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}

	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	info, err := os.Stat(p)
	if err != nil {
		return http.StatusNotFound, nil, err.Error()
	}
	fileEntry, ok := describeFileEntry(p, info.Name())
	if !ok {
		return http.StatusNotFound, nil, "file metadata unavailable"
	}
	data, _ := json.Marshal(fileEntry)
	return http.StatusOK, data, ""
}

func describeFileEntry(fullPath string, displayName string) (FileEntry, bool) {
	linfo, err := os.Lstat(fullPath)
	if err != nil {
		return FileEntry{}, false
	}
	info := linfo
	entryType := "file"
	linkTarget := ""
	if linfo.IsDir() {
		entryType = "dir"
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		entryType = "symlink"
		if target, err := os.Readlink(fullPath); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(fullPath), target)
			}
			linkTarget = filepath.Clean(target)
		}
		if targetInfo, err := os.Stat(fullPath); err == nil {
			info = targetInfo
			if targetInfo.IsDir() {
				entryType = "symlink-dir"
			}
		}
	}
	linkCount, inode := fileLinkMetadata(info)
	hardLink := entryType == "file" && linkCount > 1
	return FileEntry{
		Name:       displayName,
		Type:       entryType,
		Size:       info.Size(),
		Mode:       info.Mode().String(),
		ModTime:    info.ModTime().Format(time.RFC3339),
		LinkTarget: linkTarget,
		ChildCount: directoryChildCount(fullPath, entryType),
		HardLink:   hardLink,
		LinkCount:  linkCount,
		Inode:      inode,
	}, true
}

func directoryChildCount(fullPath string, entryType string) *int {
	if entryType != "dir" && entryType != "symlink-dir" {
		return nil
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil
	}
	count := len(entries)
	return &count
}

func fileLinkMetadata(info os.FileInfo) (linkCount uint64, inode uint64) {
	if info == nil || info.Sys() == nil {
		return 0, 0
	}
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 0, 0
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, 0
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, 0
	}
	if field := value.FieldByName("Nlink"); field.IsValid() {
		linkCount = reflectUint(field)
	}
	if field := value.FieldByName("Ino"); field.IsValid() {
		inode = reflectUint(field)
	}
	return linkCount, inode
}

func reflectUint(value reflect.Value) uint64 {
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.Int() > 0 {
			return uint64(value.Int())
		}
	}
	return 0
}

func (m *Manager) handleMkdir(body []byte) (int32, []byte, string) {
	var req struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}
	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	data, _ := json.Marshal(map[string]string{"path": p})
	return http.StatusOK, data, ""
}

func (m *Manager) handleDelete(body []byte) (int32, []byte, string) {
	var req struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}
	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	if p == "/" {
		return http.StatusForbidden, nil, "cannot delete root directory"
	}
	if err := os.RemoveAll(p); err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	data, _ := json.Marshal(map[string]string{"path": p})
	return http.StatusOK, data, ""
}

func (m *Manager) handleRename(body []byte) (int32, []byte, string) {
	var req struct {
		Path    string `json:"path"`
		NewPath string `json:"new_path"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" || req.NewPath == "" {
		return http.StatusBadRequest, nil, "path and new_path required"
	}
	oldPath, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	newPath, err := validatePath(req.NewPath)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	data, _ := json.Marshal(map[string]string{"path": newPath})
	return http.StatusOK, data, ""
}

func (m *Manager) handleCopy(body []byte) (int32, []byte, string) {
	var req struct {
		Paths []string `json:"paths"`
		Dest  string   `json:"dest"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Paths) == 0 || req.Dest == "" {
		return http.StatusBadRequest, nil, "paths and dest required"
	}
	destDir, err := validatePath(req.Dest)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	info, err := os.Stat(destDir)
	if err != nil || !info.IsDir() {
		return http.StatusBadRequest, nil, "dest is not a directory"
	}

	type result struct {
		Source string `json:"source"`
		Dest   string `json:"dest,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(req.Paths))
	copied := 0
	for _, p := range req.Paths {
		srcPath, err := validatePath(p)
		if err != nil {
			results = append(results, result{Source: p, Error: err.Error()})
			continue
		}
		destPath := resolveConflict(filepath.Join(destDir, filepath.Base(srcPath)))
		if err := copyFileOrDir(srcPath, destPath); err != nil {
			results = append(results, result{Source: srcPath, Error: err.Error()})
			continue
		}
		results = append(results, result{Source: srcPath, Dest: destPath})
		copied++
	}
	data, _ := json.Marshal(map[string]any{"copied": copied, "results": results})
	return http.StatusOK, data, ""
}

func (m *Manager) handleMove(body []byte) (int32, []byte, string) {
	var req struct {
		Paths []string `json:"paths"`
		Dest  string   `json:"dest"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Paths) == 0 || req.Dest == "" {
		return http.StatusBadRequest, nil, "paths and dest required"
	}
	destDir, err := validatePath(req.Dest)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	info, err := os.Stat(destDir)
	if err != nil || !info.IsDir() {
		return http.StatusBadRequest, nil, "dest is not a directory"
	}

	type result struct {
		Source string `json:"source"`
		Dest   string `json:"dest,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(req.Paths))
	moved := 0
	for _, p := range req.Paths {
		srcPath, err := validatePath(p)
		if err != nil {
			results = append(results, result{Source: p, Error: err.Error()})
			continue
		}
		destPath := resolveConflict(filepath.Join(destDir, filepath.Base(srcPath)))
		if err := os.Rename(srcPath, destPath); err != nil {
			results = append(results, result{Source: srcPath, Error: err.Error()})
			continue
		}
		results = append(results, result{Source: srcPath, Dest: destPath})
		moved++
	}
	data, _ := json.Marshal(map[string]any{"moved": moved, "results": results})
	return http.StatusOK, data, ""
}

func (m *Manager) handleBatchDelete(body []byte) (int32, []byte, string) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Paths) == 0 {
		return http.StatusBadRequest, nil, "paths required"
	}
	type errResult struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	errorsOut := make([]errResult, 0)
	deleted := 0
	for _, p := range req.Paths {
		path, err := validatePath(p)
		if err != nil {
			errorsOut = append(errorsOut, errResult{Path: p, Error: err.Error()})
			continue
		}
		if path == "/" {
			errorsOut = append(errorsOut, errResult{Path: p, Error: "cannot delete root directory"})
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			errorsOut = append(errorsOut, errResult{Path: path, Error: err.Error()})
			continue
		}
		deleted++
	}
	data, _ := json.Marshal(map[string]any{"deleted": deleted, "errors": errorsOut})
	return http.StatusOK, data, ""
}

func (m *Manager) handlePreview(body []byte) (int32, []byte, string) {
	var req struct {
		Path    string `json:"path"`
		MaxSize int64  `json:"max_size"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}

	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	info, err := os.Stat(p)
	if err != nil {
		return http.StatusNotFound, nil, err.Error()
	}
	if info.IsDir() {
		return http.StatusBadRequest, nil, "cannot preview a directory"
	}
	category, mimeType := detectFileCategory(info.Name())
	resp := map[string]any{
		"path":      p,
		"name":      info.Name(),
		"size":      info.Size(),
		"mime_type": mimeType,
		"category":  category,
		"is_text":   category == "text",
	}
	if category == "unsupported" {
		data, _ := json.Marshal(resp)
		return http.StatusOK, data, ""
	}

	maxText := int64(5 << 20)
	if req.MaxSize > 0 {
		if category == "text" && req.MaxSize < maxText {
			maxText = req.MaxSize
		}
	}
	if category == "text" && info.Size() > maxText {
		resp["preview_limit"] = maxText
		data, _ := json.Marshal(resp)
		return http.StatusOK, data, ""
	}
	if category == "video" {
		data, _ := json.Marshal(resp)
		return http.StatusOK, data, ""
	}

	content, err := os.ReadFile(p)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	if category == "text" {
		resp["content"] = string(content)
	} else {
		resp["content_base64"] = base64.StdEncoding.EncodeToString(content)
	}
	data, _ := json.Marshal(resp)
	return http.StatusOK, data, ""
}

func (m *Manager) handleDownloadInit(body []byte) (int32, []byte, string) {
	var req struct {
		Path       string `json:"path"`
		Offset     int64  `json:"offset"`
		Length     int64  `json:"length"`
		TransferID string `json:"transfer_id"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}
	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	info, err := os.Stat(p)
	if err != nil {
		return http.StatusNotFound, nil, err.Error()
	}
	if info.IsDir() {
		return http.StatusBadRequest, nil, "cannot download a directory"
	}

	offset := req.Offset
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	length := req.Length
	available := info.Size() - offset
	if length <= 0 || length > available {
		length = available
	}
	transferID := strings.TrimSpace(req.TransferID)
	if transferID == "" {
		transferID = fmt.Sprintf("dl-%d", time.Now().UnixNano())
	} else if !validTransferID(transferID) {
		return http.StatusBadRequest, nil, "invalid transfer_id"
	}
	m.mu.Lock()
	m.transfers[transferID] = &FileTransfer{
		ID:        transferID,
		Path:      p,
		Size:      info.Size(),
		ChunkSize: chunkSize,
		Direction: "download",
		Offset:    offset,
		Length:    length,
		CreatedAt: time.Now(),
	}
	m.mu.Unlock()

	data, _ := json.Marshal(map[string]any{
		"transfer_id": transferID,
		"name":        info.Name(),
		"size":        info.Size(),
		"chunk_size":  chunkSize,
		"offset":      offset,
		"length":      length,
	})
	return http.StatusOK, data, ""
}

func validTransferID(id string) bool {
	if id == "" || len(id) > 160 {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (m *Manager) handleUploadInit(body []byte) (int32, []byte, string) {
	var req struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		ResumeID string `json:"resume_id"`
	}
	if json.Unmarshal(body, &req) != nil || req.Path == "" {
		return http.StatusBadRequest, nil, "path required"
	}
	if req.Size < 0 {
		return http.StatusBadRequest, nil, "size must be non-negative"
	}

	p, err := validatePath(req.Path)
	if err != nil {
		return http.StatusBadRequest, nil, err.Error()
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}

	if req.ResumeID != "" {
		m.mu.Lock()
		existing, ok := m.transfers[req.ResumeID]
		m.mu.Unlock()
		if ok && existing.Direction == "upload" && existing.TempPath != "" {
			offset := existing.Offset
			if info, err := os.Stat(existing.TempPath); err == nil {
				if info.Size() > offset {
					offset = info.Size()
				}
			}
			existing.Offset = offset
			data, _ := json.Marshal(map[string]any{
				"transfer_id":     req.ResumeID,
				"chunk_size":      chunkSize,
				"uploaded_offset": offset,
			})
			return http.StatusOK, data, ""
		}
	}

	transferID := fmt.Sprintf("ul-%d", time.Now().UnixNano())
	tempPath := ""
	offset := int64(0)
	if req.ResumeID != "" {
		transferID = req.ResumeID
		tempPath = uploadResumeTempPath(p, req.ResumeID)
		if info, err := os.Stat(tempPath); err == nil {
			offset = info.Size()
			if offset > req.Size {
				_ = os.Remove(tempPath)
				offset = 0
			}
		}
	} else {
		tmpFile, err := os.CreateTemp(dir, ".termx-upload-*")
		if err != nil {
			return http.StatusInternalServerError, nil, err.Error()
		}
		tempPath = tmpFile.Name()
		_ = tmpFile.Close()
	}

	m.mu.Lock()
	m.transfers[transferID] = &FileTransfer{
		ID:        transferID,
		Path:      p,
		Size:      req.Size,
		ChunkSize: chunkSize,
		Direction: "upload",
		TempPath:  tempPath,
		Offset:    offset,
		CreatedAt: time.Now(),
	}
	m.mu.Unlock()

	data, _ := json.Marshal(map[string]any{
		"transfer_id":     transferID,
		"chunk_size":      chunkSize,
		"uploaded_offset": offset,
	})
	return http.StatusOK, data, ""
}

func (m *Manager) handleUploadComplete(body []byte) (int32, []byte, string) {
	var req struct {
		TransferID string `json:"transfer_id"`
	}
	if json.Unmarshal(body, &req) != nil || req.TransferID == "" {
		return http.StatusBadRequest, nil, "transfer_id required"
	}

	m.mu.Lock()
	transfer, ok := m.transfers[req.TransferID]
	if ok {
		delete(m.transfers, req.TransferID)
	}
	m.mu.Unlock()
	if !ok {
		return http.StatusNotFound, nil, "transfer not found"
	}
	if transfer.Direction != "upload" || transfer.TempPath == "" {
		return http.StatusBadRequest, nil, "transfer is not an upload"
	}
	if err := os.Rename(transfer.TempPath, transfer.Path); err != nil {
		_ = os.Remove(transfer.TempPath)
		return http.StatusInternalServerError, nil, err.Error()
	}
	data, _ := json.Marshal(map[string]string{"path": transfer.Path})
	return http.StatusOK, data, ""
}

func (m *Manager) HandleFileChannel(dc *webrtc.DataChannel, transferID string) {
	m.HandleFileChannelWithOpenGuard(dc, transferID, nil)
}

func (m *Manager) HandleFileChannelWithOpenGuard(dc *webrtc.DataChannel, transferID string, guard func() bool) {
	m.mu.Lock()
	transfer, ok := m.transfers[transferID]
	m.mu.Unlock()
	if !ok {
		dc.OnOpen(func() {
			sendErrorFrame(dc, "transfer not found: "+transferID)
			_ = dc.Close()
		})
		return
	}
	if transfer.Direction == "download" {
		m.handleDownloadChannel(dc, transfer, guard)
		return
	}
	m.handleUploadChannel(dc, transfer, guard)
}

func (m *Manager) handleDownloadChannel(dc *webrtc.DataChannel, transfer *FileTransfer, guard func() bool) {
	rateLimitBPS := downloadRateLimitBPS()

	dc.SetBufferedAmountLowThreshold(downloadSendBufferLow)
	dc.OnOpen(func() {
		if guard != nil && !guard() {
			_ = dc.Close()
			return
		}
		go func() {
			defer func() {
				m.mu.Lock()
				delete(m.transfers, transfer.ID)
				m.mu.Unlock()
				_ = dc.Close()
			}()

			file, err := os.Open(transfer.Path)
			if err != nil {
				sendErrorFrame(dc, err.Error())
				return
			}
			defer file.Close()

			if transfer.Offset > 0 {
				if _, err := file.Seek(transfer.Offset, io.SeekStart); err != nil {
					sendErrorFrame(dc, "seek failed: "+err.Error())
					return
				}
			}

			drainCh := make(chan struct{}, 1)
			dc.OnBufferedAmountLow(func() {
				select {
				case drainCh <- struct{}{}:
				default:
				}
			})

			buf := make([]byte, transfer.ChunkSize)
			chunkNum := uint32(transfer.Offset / int64(transfer.ChunkSize))
			sentBytes := int64(0)
			remainingBytes := transfer.Length
			sendStartedAt := time.Now()
			for remainingBytes > 0 {
				readSize := len(buf)
				if int64(readSize) > remainingBytes {
					readSize = int(remainingBytes)
				}
				n, readErr := file.Read(buf[:readSize])
				if n > 0 {
					if dc.ReadyState() != webrtc.DataChannelStateOpen {
						return
					}
					frame := make([]byte, 5+n)
					frame[0] = frameData
					binary.BigEndian.PutUint32(frame[1:5], chunkNum)
					copy(frame[5:], buf[:n])
					for dc.BufferedAmount() >= downloadSendBufferLimit {
						if dc.ReadyState() != webrtc.DataChannelStateOpen {
							return
						}
						select {
						case <-drainCh:
						case <-time.After(30 * time.Second):
							if dc.ReadyState() != webrtc.DataChannelStateOpen {
								return
							}
							sendErrorFrame(dc, "send timeout")
							return
						}
					}
					if err := dc.Send(frame); err != nil {
						sendErrorFrame(dc, err.Error())
						return
					}
					sentBytes += int64(n)
					remainingBytes -= int64(n)
					throttleDownload(sendStartedAt, sentBytes, rateLimitBPS)
					chunkNum++
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					sendErrorFrame(dc, readErr.Error())
					return
				}
			}
			completeFrame := make([]byte, 5)
			completeFrame[0] = frameComplete
			binary.BigEndian.PutUint32(completeFrame[1:5], chunkNum)
			if dc.ReadyState() == webrtc.DataChannelStateOpen {
				_ = dc.Send(completeFrame)
			}
		}()
	})
}

func downloadRateLimitBPS() int64 {
	raw := strings.TrimSpace(os.Getenv(downloadRateLimitBPSEnvName))
	if raw == "" {
		return defaultDownloadRateLimitBPS
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return defaultDownloadRateLimitBPS
	}
	return value
}

func throttleDownload(startedAt time.Time, sentBytes int64, rateLimitBPS int64) {
	if rateLimitBPS <= 0 || sentBytes <= 0 {
		return
	}
	expectedElapsed := time.Duration(sentBytes * int64(time.Second) / rateLimitBPS)
	if delay := expectedElapsed - time.Since(startedAt); delay > 0 {
		time.Sleep(delay)
	}
}

func sendErrorFrame(dc *webrtc.DataChannel, msg string) {
	frame := make([]byte, 1+len(msg))
	frame[0] = frameError
	copy(frame[1:], msg)
	_ = dc.Send(frame)
}

func (m *Manager) handleUploadChannel(dc *webrtc.DataChannel, transfer *FileTransfer, guard func() bool) {
	var (
		mu      sync.Mutex
		tmpFile *os.File
		ready   bool
		pending [][]byte
	)

	dc.OnOpen(func() {
		if guard != nil && !guard() {
			_ = dc.Close()
			return
		}
		openFlags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if transfer.Offset > 0 {
			openFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		}
		file, err := os.OpenFile(transfer.TempPath, openFlags, 0o644)
		if err != nil {
			sendErrorFrame(dc, err.Error())
			_ = dc.Close()
			return
		}

		mu.Lock()
		tmpFile = file
		for _, data := range pending {
			if len(data) > 0 && data[0] == frameComplete {
				_ = file.Close()
				ack := make([]byte, 5)
				ack[0] = frameComplete
				_ = dc.Send(ack)
				continue
			}
			written := processUploadFrame(file, data)
			if written > 0 {
				m.mu.Lock()
				transfer.Offset += int64(written)
				m.mu.Unlock()
			}
		}
		pending = nil
		ready = true
		mu.Unlock()
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		data := msg.Data
		if len(data) == 0 {
			return
		}

		mu.Lock()
		if ready {
			file := tmpFile
			mu.Unlock()
			if data[0] == frameComplete {
				if file != nil {
					_ = file.Close()
				}
				ack := make([]byte, 5)
				ack[0] = frameComplete
				_ = dc.Send(ack)
				return
			}
			written := processUploadFrame(file, data)
			if written > 0 {
				m.mu.Lock()
				transfer.Offset += int64(written)
				m.mu.Unlock()
			}
			return
		}
		pending = append(pending, append([]byte(nil), data...))
		mu.Unlock()
	})

	dc.OnClose(func() {
		mu.Lock()
		if tmpFile != nil {
			_ = tmpFile.Close()
		}
		mu.Unlock()
	})
}

func processUploadFrame(file *os.File, data []byte) int {
	if file == nil || len(data) < 5 || data[0] != frameData {
		return 0
	}
	written, _ := file.Write(data[5:])
	return written
}

func uploadResumeTempPath(destPath, resumeID string) string {
	dir := filepath.Dir(destPath)
	sum := sha256.Sum256([]byte(destPath + "|" + resumeID))
	return filepath.Join(dir, ".termx-upload-"+hex.EncodeToString(sum[:])+".part")
}

func cleanupOldUploadResumeFiles() {
	// Best effort cleanup in common writable roots. Avoid walking the whole disk.
	roots := []string{os.TempDir()}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	cutoff := time.Now().Add(-resumeFileMaxAge)
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				base := filepath.Base(path)
				if base == ".cache" || base == "Downloads" || path == root {
					return nil
				}
				if path != root {
					return filepath.SkipDir
				}
			}
			if !strings.HasPrefix(filepath.Base(path), ".termx-upload-") || !strings.HasSuffix(path, ".part") {
				return nil
			}
			info, err := d.Info()
			if err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(path)
			}
			return nil
		})
	}
}

func resolveConflict(destPath string) string {
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return destPath
	}
	dir := filepath.Dir(destPath)
	base := filepath.Base(destPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func copyFileOrDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dest, info.Mode())
	}
	return copyFile(src, dest, info.Mode())
}

func copyFile(src, dest string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, mode)
}

func copyDir(src, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(dest, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyFileOrDir(filepath.Join(src, entry.Name()), filepath.Join(dest, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func detectFileCategory(name string) (string, string) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	textExts := map[string]bool{
		"txt": true, "md": true, "markdown": true, "mdx": true, "json": true, "js": true, "ts": true, "jsx": true, "tsx": true,
		"html": true, "css": true, "py": true, "go": true, "rs": true, "java": true,
		"c": true, "cpp": true, "h": true, "hpp": true, "yaml": true, "yml": true,
		"toml": true, "xml": true, "sh": true, "bash": true, "conf": true, "cfg": true,
		"ini": true, "log": true, "sql": true, "env": true, "dockerfile": true, "makefile": true,
		"gitignore": true, "mod": true, "sum": true, "lock": true, "vue": true, "svelte": true,
		"scss": true, "less": true,
	}
	imageExts := map[string]string{
		"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif",
		"svg": "image/svg+xml", "webp": "image/webp", "bmp": "image/bmp", "ico": "image/x-icon",
		"avif": "image/avif",
	}
	videoExts := map[string]string{
		"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "mov": "video/quicktime",
		"ogv": "video/ogg", "ogg": "video/ogg",
	}
	baseName := strings.ToLower(filepath.Base(name))
	noExtTextFiles := map[string]bool{
		"dockerfile": true, "makefile": true, ".gitignore": true, ".env": true, ".editorconfig": true,
	}
	if ext == "md" || ext == "markdown" || ext == "mdx" {
		return "text", "text/markdown"
	}
	if textExts[ext] || noExtTextFiles[baseName] {
		return "text", "text/plain"
	}
	if mime, ok := imageExts[ext]; ok {
		return "image", mime
	}
	if mime, ok := videoExts[ext]; ok {
		return "video", mime
	}
	return "unsupported", "application/octet-stream"
}
