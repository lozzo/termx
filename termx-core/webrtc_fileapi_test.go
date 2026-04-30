package termx

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestE2E_WebRTCFileAPIAndTransfer(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(sourcePath, []byte("hello over api\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	uploadPath := filepath.Join(dir, "uploaded.txt")
	resumePath := filepath.Join(dir, "resumed.txt")
	bigPath := filepath.Join(dir, "big.bin")
	bigData := bytes.Repeat([]byte("abcdefghij"), 10000)
	if err := os.WriteFile(bigPath, bigData, 0o644); err != nil {
		t.Fatalf("WriteFile(big) returned error: %v", err)
	}

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	apiDC, err := offerPC.CreateDataChannel("api", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(api) returned error: %v", err)
	}
	apiOpen := make(chan struct{})
	apiDC.OnOpen(func() {
		select {
		case <-apiOpen:
		default:
			close(apiOpen)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	files := fileapi.NewManager()
	answer, err := remotertc.AnswerOffer(context.Background(), hubv1.SignalingOffer{
		SessionID:  "fileapi-session",
		DeviceID:   "device-1",
		TerminalID: "unused",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, remoteInventoryProvider{}, files)
	if err != nil {
		t.Fatalf("AnswerOffer returned error: %v", err)
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}

	select {
	case <-apiOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel open")
	}

	listResp := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-list",
		Method: "POST",
		Path:   "/files/list",
		Body:   map[string]any{"path": dir},
	})
	if int(listResp.Status) != 200 {
		t.Fatalf("expected 200 from list, got %d", listResp.Status)
	}

	previewResp := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-preview",
		Method: "POST",
		Path:   "/files/preview",
		Body:   map[string]any{"path": sourcePath},
	})
	if int(previewResp.Status) != 200 {
		t.Fatalf("expected 200 from preview, got %d", previewResp.Status)
	}

	downloadInit := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-download-init",
		Method: "POST",
		Path:   "/files/download/init",
		Body:   map[string]any{"path": sourcePath},
	})
	var downloadInitBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, downloadInit.Body, &downloadInitBody)
	if downloadInitBody.TransferID == "" {
		t.Fatal("expected download transfer id")
	}

	downloadDC, err := offerPC.CreateDataChannel("file:"+downloadInitBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(download) returned error: %v", err)
	}
	downloadBytes := readDownloadChannel(t, downloadDC)
	if string(downloadBytes) != "hello over api\n" {
		t.Fatalf("unexpected download payload: %q", string(downloadBytes))
	}

	offsetDownloadInit := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-download-offset-init",
		Method: "POST",
		Path:   "/files/download/init",
		Body:   map[string]any{"path": sourcePath, "offset": 6},
	})
	var offsetDownloadBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, offsetDownloadInit.Body, &offsetDownloadBody)
	if offsetDownloadBody.TransferID == "" {
		t.Fatal("expected offset download transfer id")
	}
	offsetDownloadDC, err := offerPC.CreateDataChannel("file:"+offsetDownloadBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(offset download) returned error: %v", err)
	}
	offsetDownloadBytes := readDownloadChannel(t, offsetDownloadDC)
	if string(offsetDownloadBytes) != "over api\n" {
		t.Fatalf("unexpected offset download payload: %q", string(offsetDownloadBytes))
	}

	uploadInit := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-upload-init",
		Method: "POST",
		Path:   "/files/upload/init",
		Body:   map[string]any{"path": uploadPath, "size": 17},
	})
	var uploadInitBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, uploadInit.Body, &uploadInitBody)
	if uploadInitBody.TransferID == "" {
		t.Fatal("expected upload transfer id")
	}

	uploadDC, err := offerPC.CreateDataChannel("file:"+uploadInitBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(upload) returned error: %v", err)
	}
	sendUploadChannel(t, uploadDC, []byte("uploaded content\n"))

	uploadComplete := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-upload-complete",
		Method: "POST",
		Path:   "/files/upload/complete",
		Body:   map[string]any{"transfer_id": uploadInitBody.TransferID},
	})
	if int(uploadComplete.Status) != 200 {
		t.Fatalf("expected 200 from upload complete, got %d", uploadComplete.Status)
	}

	uploaded, err := os.ReadFile(uploadPath)
	if err != nil {
		t.Fatalf("ReadFile(uploaded) returned error: %v", err)
	}
	if string(uploaded) != "uploaded content\n" {
		t.Fatalf("unexpected uploaded payload: %q", string(uploaded))
	}

	downloadResumeInit := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-download-resume-init",
		Method: "POST",
		Path:   "/files/download/init",
		Body:   map[string]any{"path": bigPath},
	})
	var downloadResumeBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, downloadResumeInit.Body, &downloadResumeBody)
	if downloadResumeBody.TransferID == "" {
		t.Fatal("expected resume download transfer id")
	}
	partialDC, err := offerPC.CreateDataChannel("file:"+downloadResumeBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(partial download) returned error: %v", err)
	}
	partialBytes := readDownloadPartial(t, partialDC, 1)
	if len(partialBytes) == 0 {
		t.Fatal("expected partial download bytes")
	}

	downloadResumeContinue := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-download-resume-continue",
		Method: "POST",
		Path:   "/files/download/init",
		Body:   map[string]any{"path": bigPath, "offset": len(partialBytes)},
	})
	var downloadResumeContinueBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, downloadResumeContinue.Body, &downloadResumeContinueBody)
	resumeDC, err := offerPC.CreateDataChannel("file:"+downloadResumeContinueBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(resume download) returned error: %v", err)
	}
	resumedBytes := readDownloadChannel(t, resumeDC)
	combined := append(append([]byte(nil), partialBytes...), resumedBytes...)
	if !bytes.Equal(combined, bigData) {
		t.Fatalf("unexpected resumed download payload length=%d want=%d", len(combined), len(bigData))
	}

	partialInit := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-upload-resume-init",
		Method: "POST",
		Path:   "/files/upload/init",
		Body:   map[string]any{"path": resumePath, "size": 15},
	})
	var partialInitBody struct {
		TransferID string `json:"transfer_id"`
	}
	decodeResponseBody(t, partialInit.Body, &partialInitBody)
	if partialInitBody.TransferID == "" {
		t.Fatal("expected resume upload transfer id")
	}

	partialDC, err = offerPC.CreateDataChannel("file:"+partialInitBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(partial upload) returned error: %v", err)
	}
	sendUploadPartial(t, partialDC, []byte("resume "), false)
	_ = partialDC.Close()

	var resumeInitBody struct {
		TransferID     string `json:"transfer_id"`
		UploadedOffset int64  `json:"uploaded_offset"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		resumeInit := sendAPIRequest(t, apiDC, apiRequestPayload{
			ID:     "req-upload-resume-reinit",
			Method: "POST",
			Path:   "/files/upload/init",
			Body:   map[string]any{"path": resumePath, "size": 15, "resume_id": partialInitBody.TransferID},
		})
		decodeResponseBody(t, resumeInit.Body, &resumeInitBody)
		if resumeInitBody.UploadedOffset == 7 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resumeInitBody.TransferID != partialInitBody.TransferID {
		t.Fatalf("expected same resume transfer id, got %q != %q", resumeInitBody.TransferID, partialInitBody.TransferID)
	}
	if resumeInitBody.UploadedOffset != 7 {
		t.Fatalf("expected uploaded offset 7, got %d", resumeInitBody.UploadedOffset)
	}

	resumeDC, err = offerPC.CreateDataChannel("file:"+resumeInitBody.TransferID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(resume upload) returned error: %v", err)
	}
	sendUploadPartial(t, resumeDC, []byte("works!!!"), true)

	resumeComplete := sendAPIRequest(t, apiDC, apiRequestPayload{
		ID:     "req-upload-resume-complete",
		Method: "POST",
		Path:   "/files/upload/complete",
		Body:   map[string]any{"transfer_id": resumeInitBody.TransferID},
	})
	if int(resumeComplete.Status) != 200 {
		t.Fatalf("expected 200 from resume upload complete, got %d", resumeComplete.Status)
	}
	resumed, err := os.ReadFile(resumePath)
	if err != nil {
		t.Fatalf("ReadFile(resumed) returned error: %v", err)
	}
	if string(resumed) != "resume works!!!" {
		t.Fatalf("unexpected resumed payload: %q", string(resumed))
	}
}

type apiRequestPayload struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Body   interface{} `json:"body"`
}

type apiResponsePayload struct {
	ID     string          `json:"id"`
	Status int32           `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func sendAPIRequest(t *testing.T, dc *webrtc.DataChannel, payload apiRequestPayload) apiResponsePayload {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	resultCh := make(chan apiResponsePayload, 1)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		resp, ok := decodeChunkedAPIResponse(msg.Data)
		if ok {
			resultCh <- resp
		}
	})
	if err := dc.Send(raw); err != nil {
		t.Fatalf("api datachannel send returned error: %v", err)
	}
	select {
	case resp := <-resultCh:
		return resp
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api response")
		return apiResponsePayload{}
	}
}

func decodeChunkedAPIResponse(data []byte) (apiResponsePayload, bool) {
	if len(data) < 4 || data[0] != 0xc0 {
		return apiResponsePayload{}, false
	}
	idLen := int(data[2])
	if len(data) < 3+idLen {
		return apiResponsePayload{}, false
	}
	var out apiResponsePayload
	if err := json.Unmarshal(data[3+idLen:], &out); err != nil {
		return apiResponsePayload{}, false
	}
	return out, true
}

func decodeResponseBody(t *testing.T, body json.RawMessage, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json.Unmarshal(body) returned error: %v", err)
	}
}

func readDownloadChannel(t *testing.T, dc *webrtc.DataChannel) []byte {
	t.Helper()
	return readDownloadPartial(t, dc, -1)
}

func readDownloadPartial(t *testing.T, dc *webrtc.DataChannel, closeAfterChunks int) []byte {
	t.Helper()
	openCh := make(chan struct{})
	doneCh := make(chan []byte, 1)
	dc.OnOpen(func() {
		select {
		case <-openCh:
		default:
			close(openCh)
		}
	})
	var chunks [][]byte
	chunkCount := 0
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) == 0 {
			return
		}
		switch msg.Data[0] {
		case 0x01:
			chunks = append(chunks, append([]byte(nil), msg.Data[5:]...))
			chunkCount++
			if closeAfterChunks > 0 && chunkCount >= closeAfterChunks {
				total := 0
				for _, chunk := range chunks {
					total += len(chunk)
				}
				merged := make([]byte, 0, total)
				for _, chunk := range chunks {
					merged = append(merged, chunk...)
				}
				_ = dc.Close()
				doneCh <- merged
			}
		case 0x02:
			total := 0
			for _, chunk := range chunks {
				total += len(chunk)
			}
			merged := make([]byte, 0, total)
			for _, chunk := range chunks {
				merged = append(merged, chunk...)
			}
			doneCh <- merged
		}
	})
	select {
	case <-openCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for file download channel open")
	}
	select {
	case data := <-doneCh:
		return data
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for file download data")
		return nil
	}
}

func sendUploadChannel(t *testing.T, dc *webrtc.DataChannel, payload []byte) {
	t.Helper()
	sendUploadPartial(t, dc, payload, true)
}

func sendUploadPartial(t *testing.T, dc *webrtc.DataChannel, payload []byte, complete bool) {
	t.Helper()
	openCh := make(chan struct{})
	ackCh := make(chan struct{}, 1)
	dc.OnOpen(func() {
		select {
		case <-openCh:
		default:
			close(openCh)
		}
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) > 0 && msg.Data[0] == 0x02 {
			select {
			case ackCh <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-openCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for file upload channel open")
	}

	frame := make([]byte, 5+len(payload))
	frame[0] = 0x01
	copy(frame[5:], payload)
	if err := dc.Send(frame); err != nil {
		t.Fatalf("upload channel send returned error: %v", err)
	}
	if !complete {
		return
	}
	completeFrame := make([]byte, 5)
	completeFrame[0] = 0x02
	if err := dc.Send(completeFrame); err != nil {
		t.Fatalf("upload complete frame send returned error: %v", err)
	}
	select {
	case <-ackCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for upload ack")
	}
}
