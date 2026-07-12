package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
)

const fileTransferChunkBytes = 64 << 10
const fileTransferWindowBytes = 256 << 10
const fileUploadResumeTTL = 15 * time.Minute

type fileTransferDirection uint8

const (
	fileTransferDownload fileTransferDirection = iota + 1
	fileTransferUpload
)

type uploadTransferRecord struct {
	ID                string
	PrincipalID       string
	TargetPath        string
	TempPath          string
	Size              int64
	Offset            int64
	Overwrite         bool
	ExpiresAt         time.Time
	AttachedSessionID uint64
	attachedSession   *protocolSession
}

type sessionFileTransfer struct {
	mu        sync.Mutex
	id        string
	channel   uint16
	direction fileTransferDirection
	path      string
	file      *os.File
	offset    int64
	size      int64
	hasher    hash.Hash
	ack       chan protocol.FileTransferAck
	cancel    context.CancelFunc
}

func (session *protocolSession) openFileDownload(ctx context.Context, params protocol.FileDownloadOpenParams) (protocol.FileTransferOpenResult, error) {
	path, err := absoluteFilePath(params.Path)
	if err != nil {
		return protocol.FileTransferOpenResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return protocol.FileTransferOpenResult{}, err
	}
	if !info.Mode().IsRegular() {
		return protocol.FileTransferOpenResult{}, fmt.Errorf("download requires a regular file")
	}
	if params.Offset < 0 || params.Offset > info.Size() {
		return protocol.FileTransferOpenResult{}, fmt.Errorf("invalid download offset")
	}
	if params.ExpectedSize > 0 && params.ExpectedSize != info.Size() {
		return protocol.FileTransferOpenResult{}, fmt.Errorf("stale download source")
	}
	if !params.ExpectedModifiedAt.IsZero() && params.ExpectedModifiedAt.UnixNano() != info.ModTime().UnixNano() {
		return protocol.FileTransferOpenResult{}, fmt.Errorf("stale download source")
	}
	file, err := os.Open(path)
	if err != nil {
		return protocol.FileTransferOpenResult{}, err
	}
	if _, err = file.Seek(params.Offset, io.SeekStart); err != nil {
		file.Close()
		return protocol.FileTransferOpenResult{}, err
	}
	id, err := newFileTransferID()
	if err != nil {
		file.Close()
		return protocol.FileTransferOpenResult{}, err
	}
	channel := session.allocateFileChannel()
	transferCtx, cancel := context.WithCancel(ctx)
	transfer := &sessionFileTransfer{id: id, channel: channel, direction: fileTransferDownload, path: path, file: file, offset: params.Offset, size: info.Size(), ack: make(chan protocol.FileTransferAck, 1), cancel: cancel}
	session.registerFileTransfer(transfer)
	go session.runFileDownload(transferCtx, transfer)
	return protocol.FileTransferOpenResult{TransferID: id, Channel: channel, Path: path, Offset: params.Offset, Size: info.Size(), ModifiedAt: info.ModTime().UTC(), WindowBytes: fileTransferWindowBytes, ChunkBytes: fileTransferChunkBytes}, nil
}

func (session *protocolSession) runFileDownload(ctx context.Context, transfer *sessionFileTransfer) {
	defer session.releaseFileTransfer(transfer.id, false)
	credit := int64(fileTransferWindowBytes)
	buffer := make([]byte, fileTransferChunkBytes)
	for transfer.offset < transfer.size {
		for credit <= 0 {
			select {
			case <-ctx.Done():
				return
			case ack := <-transfer.ack:
				if ack.Offset != transfer.offset || ack.WindowBytes < 0 || ack.WindowBytes > fileTransferWindowBytes {
					_ = session.sendStreamError(transfer.channel, protocolErrorBadRequest, "invalid file ack")
					return
				}
				credit += ack.WindowBytes
			}
		}
		readSize := min(int64(len(buffer)), min(credit, transfer.size-transfer.offset))
		n, err := transfer.file.Read(buffer[:readSize])
		if err != nil && err != io.EOF {
			_ = session.sendStreamError(transfer.channel, protocolErrorInternal, err.Error())
			return
		}
		if n == 0 {
			break
		}
		payload, err := protocol.EncodeFileTransferData(protocol.FileTransferData{Offset: transfer.offset, Data: buffer[:n]})
		if err != nil {
			return
		}
		if err := session.sendFrame(transfer.channel, wire.TypeFileData, payload); err != nil {
			return
		}
		transfer.offset += int64(n)
		credit -= int64(n)
	}
	digest, err := hashFile(transfer.path)
	if err != nil {
		_ = session.sendStreamError(transfer.channel, protocolErrorInternal, err.Error())
		return
	}
	payload, err := protocol.EncodeFileTransferFinish(protocol.FileTransferFinish{Size: transfer.size, SHA256: digest})
	if err == nil {
		_ = session.sendFrame(transfer.channel, wire.TypeFileFinish, payload)
	}
}

func (session *protocolSession) openFileUpload(params protocol.FileUploadOpenParams) (protocol.FileTransferOpenResult, error) {
	target, err := absoluteFilePath(params.Path)
	if err != nil {
		return protocol.FileTransferOpenResult{}, err
	}
	if params.Size < 0 {
		return protocol.FileTransferOpenResult{}, fmt.Errorf("invalid upload size")
	}
	now := time.Now().UTC()
	var record *uploadTransferRecord
	session.server.fileTransferMu.Lock()
	session.server.removeExpiredUploadsLocked(now)
	if params.ResumeTransferID != "" {
		record = session.server.fileUploads[params.ResumeTransferID]
		if record == nil || record.PrincipalID != session.scope.PrincipalID || record.TargetPath != target || record.Size != params.Size || record.AttachedSessionID == session.sessionID {
			session.server.fileTransferMu.Unlock()
			return protocol.FileTransferOpenResult{}, fmt.Errorf("upload transfer is unavailable")
		}
		previousSession := record.attachedSession
		if previousSession != nil {
			session.server.fileTransferMu.Unlock()
			previousSession.releaseUploadForTakeover(record.ID)
			session.server.fileTransferMu.Lock()
			record = session.server.fileUploads[params.ResumeTransferID]
			if record == nil || record.PrincipalID != session.scope.PrincipalID || record.TargetPath != target || record.Size != params.Size || record.AttachedSessionID != 0 {
				session.server.fileTransferMu.Unlock()
				return protocol.FileTransferOpenResult{}, fmt.Errorf("upload transfer is unavailable")
			}
		}
	} else {
		if !params.Overwrite {
			if _, statErr := os.Lstat(target); statErr == nil {
				session.server.fileTransferMu.Unlock()
				return protocol.FileTransferOpenResult{}, os.ErrExist
			}
		}
		id, idErr := newFileTransferID()
		if idErr != nil {
			session.server.fileTransferMu.Unlock()
			return protocol.FileTransferOpenResult{}, idErr
		}
		temp, tempErr := os.CreateTemp(filepath.Dir(target), ".termx-upload-*.part")
		if tempErr != nil {
			session.server.fileTransferMu.Unlock()
			return protocol.FileTransferOpenResult{}, tempErr
		}
		tempPath := temp.Name()
		temp.Close()
		record = &uploadTransferRecord{ID: id, PrincipalID: session.scope.PrincipalID, TargetPath: target, TempPath: tempPath, Size: params.Size, Overwrite: params.Overwrite, ExpiresAt: now.Add(fileUploadResumeTTL)}
		session.server.fileUploads[id] = record
	}
	record.AttachedSessionID = session.sessionID
	record.attachedSession = session
	session.server.fileTransferMu.Unlock()
	file, err := os.OpenFile(record.TempPath, os.O_RDWR, 0o600)
	if err != nil {
		session.detachUploadRecord(record.ID)
		return protocol.FileTransferOpenResult{}, err
	}
	hasher := sha256.New()
	if record.Offset > 0 {
		if _, err = io.CopyN(hasher, file, record.Offset); err != nil {
			file.Close()
			session.detachUploadRecord(record.ID)
			return protocol.FileTransferOpenResult{}, err
		}
	}
	if _, err = file.Seek(record.Offset, io.SeekStart); err != nil {
		file.Close()
		session.detachUploadRecord(record.ID)
		return protocol.FileTransferOpenResult{}, err
	}
	channel := session.allocateFileChannel()
	transfer := &sessionFileTransfer{id: record.ID, channel: channel, direction: fileTransferUpload, path: target, file: file, offset: record.Offset, size: record.Size, hasher: hasher}
	session.registerFileTransfer(transfer)
	return protocol.FileTransferOpenResult{TransferID: record.ID, Channel: channel, Path: target, Offset: record.Offset, Size: record.Size, WindowBytes: fileTransferWindowBytes, ChunkBytes: fileTransferChunkBytes}, nil
}

func (session *protocolSession) handleFileTransferFrame(ctx context.Context, transfer *sessionFileTransfer, typ uint8, payload []byte) error {
	_ = ctx
	transfer.mu.Lock()
	defer transfer.mu.Unlock()
	if transfer.direction == fileTransferDownload {
		if typ != wire.TypeFileAck {
			return fmt.Errorf("download channel requires ack frame")
		}
		ack, err := protocol.DecodeFileTransferAck(payload)
		if err != nil {
			return err
		}
		if ack.Offset < 0 || ack.Offset > transfer.size || ack.WindowBytes < 0 || ack.WindowBytes > fileTransferWindowBytes {
			return fmt.Errorf("invalid download ack")
		}
		select {
		case transfer.ack <- ack:
		default:
			return fmt.Errorf("download ack backpressure exceeded")
		}
		return nil
	}
	switch typ {
	case wire.TypeFileData:
		data, err := protocol.DecodeFileTransferData(payload)
		if err != nil {
			return err
		}
		if data.Offset != transfer.offset || len(data.Data) == 0 || len(data.Data) > fileTransferChunkBytes || transfer.offset+int64(len(data.Data)) > transfer.size {
			return fmt.Errorf("invalid upload data offset or size")
		}
		if _, err := transfer.file.Write(data.Data); err != nil {
			return err
		}
		if _, err := transfer.hasher.Write(data.Data); err != nil {
			return err
		}
		transfer.offset += int64(len(data.Data))
		session.updateUploadOffset(transfer.id, transfer.offset)
		ackPayload, err := protocol.EncodeFileTransferAck(protocol.FileTransferAck{Offset: transfer.offset, WindowBytes: int64(len(data.Data))})
		if err != nil {
			return err
		}
		return session.sendFrame(transfer.channel, wire.TypeFileAck, ackPayload)
	case wire.TypeFileFinish:
		finish, err := protocol.DecodeFileTransferFinish(payload)
		if err != nil {
			return err
		}
		if finish.Size != transfer.size || transfer.offset != transfer.size || !bytes.Equal(finish.SHA256, transfer.hasher.Sum(nil)) {
			return fmt.Errorf("upload checksum mismatch")
		}
		if err := transfer.file.Sync(); err != nil {
			return err
		}
		if err := transfer.file.Close(); err != nil {
			return err
		}
		transfer.file = nil
		if err := session.publishUpload(transfer.id); err != nil {
			return err
		}
		resultPayload, err := protocol.EncodeFileTransferResult(protocol.FileTransferResult{Path: transfer.path, Size: transfer.size, SHA256: finish.SHA256})
		if err != nil {
			return err
		}
		if err := session.sendFrame(transfer.channel, wire.TypeFileResult, resultPayload); err != nil {
			return err
		}
		session.releaseFileTransfer(transfer.id, true)
		return nil
	default:
		return fmt.Errorf("unsupported upload frame type %d", typ)
	}
}

func (session *protocolSession) cancelFileTransfer(params protocol.FileTransferCancelParams) protocol.FileTransferCancelResult {
	session.fileMu.Lock()
	channel, ok := session.fileIDs[params.TransferID]
	transfer := session.fileChannels[channel]
	session.fileMu.Unlock()
	if ok {
		session.releaseFileTransfer(params.TransferID, true)
		if transfer != nil && transfer.direction == fileTransferUpload {
			session.removeUpload(params.TransferID)
		}
		return protocol.FileTransferCancelResult{Cancelled: true}
	}
	owned := session.uploadOwnedByPrincipal(params.TransferID)
	if owned {
		session.removeUpload(params.TransferID)
	}
	return protocol.FileTransferCancelResult{Cancelled: owned}
}

func (session *protocolSession) uploadOwnedByPrincipal(id string) bool {
	session.server.fileTransferMu.Lock()
	defer session.server.fileTransferMu.Unlock()
	record := session.server.fileUploads[id]
	return record != nil && record.PrincipalID == session.scope.PrincipalID
}

func (session *protocolSession) allocateFileChannel() uint16 {
	for {
		channel := uint16(session.nextCh.Add(1))
		if channel != 0 {
			session.fileMu.Lock()
			_, used := session.fileChannels[channel]
			session.fileMu.Unlock()
			if !used {
				return channel
			}
		}
	}
}
func (session *protocolSession) registerFileTransfer(transfer *sessionFileTransfer) {
	session.fileMu.Lock()
	session.fileChannels[transfer.channel] = transfer
	session.fileIDs[transfer.id] = transfer.channel
	session.fileMu.Unlock()
}
func (session *protocolSession) fileTransferForChannel(channel uint16) *sessionFileTransfer {
	session.fileMu.Lock()
	defer session.fileMu.Unlock()
	return session.fileChannels[channel]
}
func (session *protocolSession) releaseFileTransfer(id string, completed bool) {
	session.fileMu.Lock()
	channel, ok := session.fileIDs[id]
	transfer := session.fileChannels[channel]
	if ok {
		delete(session.fileIDs, id)
		delete(session.fileChannels, channel)
	}
	session.fileMu.Unlock()
	if transfer != nil {
		if transfer.cancel != nil {
			transfer.cancel()
		}
		if transfer.file != nil {
			_ = transfer.file.Close()
		}
		if transfer.direction == fileTransferUpload && !completed {
			session.detachUploadRecord(id)
		}
	}
}

func (session *protocolSession) releaseAllFileTransfers() {
	session.fileMu.Lock()
	ids := make([]string, 0, len(session.fileIDs))
	for id := range session.fileIDs {
		ids = append(ids, id)
	}
	session.fileMu.Unlock()
	for _, id := range ids {
		session.releaseFileTransfer(id, false)
	}
}

// releaseUploadForTakeover 串行关闭同 principal 旧 session 的上传 channel，使新 session 可从 daemon offset 接管。
func (session *protocolSession) releaseUploadForTakeover(id string) {
	session.fileMu.Lock()
	channel, ok := session.fileIDs[id]
	transfer := session.fileChannels[channel]
	if ok {
		delete(session.fileIDs, id)
		delete(session.fileChannels, channel)
	}
	session.fileMu.Unlock()
	if transfer != nil {
		transfer.mu.Lock()
		if transfer.file != nil {
			_ = transfer.file.Close()
			transfer.file = nil
		}
		transfer.mu.Unlock()
	}
	session.detachUploadRecord(id)
}

func (session *protocolSession) detachUploadRecord(id string) {
	session.server.fileTransferMu.Lock()
	if record := session.server.fileUploads[id]; record != nil {
		if record.AttachedSessionID == session.sessionID {
			record.AttachedSessionID = 0
			record.attachedSession = nil
		}
		record.ExpiresAt = time.Now().UTC().Add(fileUploadResumeTTL)
	}
	session.server.fileTransferMu.Unlock()
}
func (session *protocolSession) updateUploadOffset(id string, offset int64) {
	session.server.fileTransferMu.Lock()
	if record := session.server.fileUploads[id]; record != nil {
		record.Offset = offset
		record.ExpiresAt = time.Now().UTC().Add(fileUploadResumeTTL)
	}
	session.server.fileTransferMu.Unlock()
}
func (session *protocolSession) publishUpload(id string) error {
	session.server.fileTransferMu.Lock()
	record := session.server.fileUploads[id]
	if record == nil {
		session.server.fileTransferMu.Unlock()
		return fmt.Errorf("upload transfer missing")
	}
	session.server.fileTransferMu.Unlock()
	if !record.Overwrite {
		if _, err := os.Lstat(record.TargetPath); err == nil {
			return os.ErrExist
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(record.TempPath, record.TargetPath); err != nil {
		return err
	}
	session.server.fileTransferMu.Lock()
	delete(session.server.fileUploads, id)
	session.server.fileTransferMu.Unlock()
	return nil
}
func (session *protocolSession) removeUpload(id string) {
	session.server.fileTransferMu.Lock()
	record := session.server.fileUploads[id]
	delete(session.server.fileUploads, id)
	session.server.fileTransferMu.Unlock()
	if record != nil {
		_ = os.Remove(record.TempPath)
	}
}
func (server *Server) removeExpiredUploadsLocked(now time.Time) {
	for id, record := range server.fileUploads {
		if record.AttachedSessionID == 0 && !now.Before(record.ExpiresAt) {
			_ = os.Remove(record.TempPath)
			delete(server.fileUploads, id)
		}
	}
}
func newFileTransferID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func hashFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}
