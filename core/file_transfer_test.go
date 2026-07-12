package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
)

func TestProtocolFileDownloadHonorsAckWindowAndDigest(t *testing.T) {
	content := bytes.Repeat([]byte("termx-file-window-"), 32768)
	path := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()
	opened, err := client.FileDownloadOpen(context.Background(), protocol.FileDownloadOpenParams{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	stream, stop := client.Stream(opened.Channel)
	defer stop()
	var received []byte
	for int64(len(received)) < opened.WindowBytes {
		frame := waitFileStreamFrame(t, stream)
		if frame.Type != wire.TypeFileData {
			t.Fatalf("expected data, got %d", frame.Type)
		}
		data, err := protocol.DecodeFileTransferData(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if data.Offset != int64(len(received)) {
			t.Fatalf("offset %d want %d", data.Offset, len(received))
		}
		received = append(received, data.Data...)
	}
	select {
	case frame := <-stream:
		t.Fatalf("download exceeded unacked window with frame %d", frame.Type)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("stalled file stream blocked control request: %v", err)
	}
	ackPayload, err := protocol.EncodeFileTransferAck(protocol.FileTransferAck{Offset: int64(len(received)), WindowBytes: fileTransferWindowBytes})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendFileFrame(opened.Channel, wire.TypeFileAck, ackPayload); err != nil {
		t.Fatal(err)
	}
	bytesSinceAck := int64(0)
	for {
		frame := waitFileStreamFrame(t, stream)
		if frame.Type == wire.TypeFileFinish {
			finish, err := protocol.DecodeFileTransferFinish(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			want := sha256.Sum256(content)
			if finish.Size != int64(len(content)) || !bytes.Equal(finish.SHA256, want[:]) {
				t.Fatalf("finish %#v", finish)
			}
			break
		}
		data, err := protocol.DecodeFileTransferData(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if data.Offset != int64(len(received)) {
			t.Fatalf("offset %d want %d", data.Offset, len(received))
		}
		received = append(received, data.Data...)
		bytesSinceAck += int64(len(data.Data))
		if bytesSinceAck == fileTransferWindowBytes {
			ackPayload, err := protocol.EncodeFileTransferAck(protocol.FileTransferAck{Offset: int64(len(received)), WindowBytes: fileTransferWindowBytes})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.SendFileFrame(opened.Channel, wire.TypeFileAck, ackPayload); err != nil {
				t.Fatal(err)
			}
			bytesSinceAck = 0
		}
	}
	if !bytes.Equal(received, content) {
		t.Fatalf("download mismatch: %d != %d", len(received), len(content))
	}
}

func TestProtocolFileDownloadRejectsChangedSourceIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changed.bin")
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()
	_, err := client.FileDownloadOpen(context.Background(), protocol.FileDownloadOpenParams{Path: path, ExpectedSize: 99})
	if err == nil || !strings.Contains(err.Error(), "stale download source") {
		t.Fatalf("expected stale source, got %v", err)
	}
}

func TestProtocolFileUploadResumesAcrossSessionAndValidatesDigest(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	target := filepath.Join(t.TempDir(), "uploaded.bin")
	content := bytes.Repeat([]byte("resume-me-"), 20000)
	_, first, closeFirst := newProtocolClientWithServer(t, server)
	opened, err := first.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: target, Size: int64(len(content))})
	if err != nil {
		t.Fatal(err)
	}
	firstStream, stopFirst := first.Stream(opened.Channel)
	firstChunk := content[:fileTransferChunkBytes]
	sendUploadData(t, first, opened.Channel, 0, firstChunk)
	ack := waitUploadAck(t, firstStream)
	if ack.Offset != int64(len(firstChunk)) {
		t.Fatalf("first ack %#v", ack)
	}
	stopFirst()
	closeFirst()

	_, second, closeSecond := newProtocolClientWithServer(t, server)
	defer closeSecond()
	resumed, err := second.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: target, Size: int64(len(content)), ResumeTransferID: opened.TransferID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Offset != int64(len(firstChunk)) {
		t.Fatalf("resume offset %d", resumed.Offset)
	}
	stream, stop := second.Stream(resumed.Channel)
	defer stop()
	offset := len(firstChunk)
	for offset < len(content) {
		end := min(offset+fileTransferChunkBytes, len(content))
		sendUploadData(t, second, resumed.Channel, int64(offset), content[offset:end])
		ack := waitUploadAck(t, stream)
		if ack.Offset != int64(end) {
			t.Fatalf("ack offset %d want %d", ack.Offset, end)
		}
		offset = end
	}
	bad := bytes.Repeat([]byte{0xff}, 32)
	finishPayload, err := protocol.EncodeFileTransferFinish(protocol.FileTransferFinish{Size: int64(len(content)), SHA256: bad})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.SendFileFrame(resumed.Channel, wire.TypeFileFinish, finishPayload); err != nil {
		t.Fatal(err)
	}
	errorFrame := waitFileStreamFrame(t, stream)
	if errorFrame.Type != wire.TypeError {
		t.Fatalf("expected checksum error, got %d", errorFrame.Type)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("checksum failure published target: %v", err)
	}
	want := sha256.Sum256(content)
	finishPayload, err = protocol.EncodeFileTransferFinish(protocol.FileTransferFinish{Size: int64(len(content)), SHA256: want[:]})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.SendFileFrame(resumed.Channel, wire.TypeFileFinish, finishPayload); err != nil {
		t.Fatal(err)
	}
	resultFrame := waitFileStreamFrame(t, stream)
	if resultFrame.Type != wire.TypeFileResult {
		t.Fatalf("expected result, got %d", resultFrame.Type)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, content) {
		t.Fatalf("uploaded content %d %v", len(data), err)
	}
}

func TestProtocolFileUploadCancelIsIdempotentAndCleansTemp(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	targetDir := t.TempDir()
	opened, err := client.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: filepath.Join(targetDir, "cancelled.bin"), Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FileTransferCancel(context.Background(), opened.TransferID)
	if err != nil || !result.Cancelled {
		t.Fatalf("cancel %#v %v", result, err)
	}
	result, err = client.FileTransferCancel(context.Background(), opened.TransferID)
	if err != nil || result.Cancelled {
		t.Fatalf("second cancel %#v %v", result, err)
	}
	server.fileTransferMu.Lock()
	count := len(server.fileUploads)
	server.fileTransferMu.Unlock()
	if count != 0 {
		t.Fatalf("upload registry leaked %d records", count)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".termx-upload-") {
			t.Fatalf("temporary upload leaked: %s", entry.Name())
		}
	}
}

func TestProtocolFileUploadResumeIsBoundToVerifiedPrincipal(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	scopeA := fullDaemonTransportScope()
	scopeA.PrincipalID = "grant-a"
	clientA, closeA := newClientForServedTransport(t, server, scopeA, true)
	target := filepath.Join(t.TempDir(), "principal.bin")
	opened, err := clientA.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: target, Size: 3})
	if err != nil {
		t.Fatal(err)
	}
	closeA()
	scopeB := fullDaemonTransportScope()
	scopeB.PrincipalID = "grant-b"
	clientB, closeB := newClientForServedTransport(t, server, scopeB, true)
	defer closeB()
	if _, err := clientB.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: target, Size: 3, ResumeTransferID: opened.TransferID}); err == nil {
		t.Fatal("different principal resumed upload")
	}
	cancelled, err := clientB.FileTransferCancel(context.Background(), opened.TransferID)
	if err != nil || cancelled.Cancelled {
		t.Fatalf("different principal cancelled upload: %#v %v", cancelled, err)
	}
	clientA2, closeA2 := newClientForServedTransport(t, server, scopeA, true)
	defer closeA2()
	resumed, err := clientA2.FileUploadOpen(context.Background(), protocol.FileUploadOpenParams{Path: target, Size: 3, ResumeTransferID: opened.TransferID})
	if err != nil || resumed.Offset != 0 {
		t.Fatalf("owner resume %#v %v", resumed, err)
	}
}

func sendUploadData(t *testing.T, client *protocol.Client, channel uint16, offset int64, data []byte) {
	t.Helper()
	payload, err := protocol.EncodeFileTransferData(protocol.FileTransferData{Offset: offset, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendFileFrame(channel, wire.TypeFileData, payload); err != nil {
		t.Fatal(err)
	}
}

func waitUploadAck(t *testing.T, stream <-chan protocol.StreamFrame) protocol.FileTransferAck {
	t.Helper()
	frame := waitFileStreamFrame(t, stream)
	if frame.Type != wire.TypeFileAck {
		t.Fatalf("expected ack, got %d", frame.Type)
	}
	ack, err := protocol.DecodeFileTransferAck(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return ack
}

func waitFileStreamFrame(t *testing.T, stream <-chan protocol.StreamFrame) protocol.StreamFrame {
	t.Helper()
	select {
	case frame, ok := <-stream:
		if !ok {
			t.Fatal("file stream closed")
		}
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file stream")
	}
	return protocol.StreamFrame{}
}
