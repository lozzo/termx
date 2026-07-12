package protocol

import (
	"reflect"
	"testing"
	"time"
)

func TestFileMethodCodecRoundTrips(t *testing.T) {
	paramsCases := []struct {
		method string
		value  any
	}{
		{"file.list", FileListParams{Path: "/tmp", Cursor: "next", Limit: 20}},
		{"file.stat", FilePathParams{Path: "/tmp/a"}},
		{"file.preview", FilePreviewParams{Path: "/tmp/a", MaxBytes: 32}},
		{"file.mkdir", FilePathParams{Path: "/tmp/d", Recursive: true}},
		{"file.rename", FileRenameParams{Path: "/tmp/a", NewPath: "/tmp/b", Overwrite: true}},
		{"file.copy", FileCopyMoveParams{Paths: []string{"/tmp/a"}, TargetDir: "/tmp/d"}},
		{"file.download.open", FileDownloadOpenParams{Path: "/tmp/a", Offset: 12, ExpectedSize: 30, ExpectedModifiedAt: time.Unix(1, 2).UTC()}},
		{"file.upload.open", FileUploadOpenParams{Path: "/tmp/a", Size: 30, Overwrite: true, ResumeTransferID: "resume"}},
		{"file.transfer.cancel", FileTransferCancelParams{TransferID: "transfer"}},
	}
	for _, test := range paramsCases {
		t.Run(test.method+" params", func(t *testing.T) {
			payload, err := EncodeMethodParams(test.method, test.value)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeMethodParams(test.method, payload)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.value) {
				t.Fatalf("got %#v want %#v", got, test.value)
			}
		})
	}
	now := time.Unix(12, 34).UTC()
	resultCases := []struct {
		method string
		value  any
		out    any
	}{
		{"file.list", FileListResult{Path: "/tmp", Entries: []FileEntry{{Path: "/tmp/a", Name: "a", Type: "file", Size: 2, ModifiedAt: now}}, NextCursor: "n"}, &FileListResult{}},
		{"file.stat", FileEntry{Path: "/tmp/a", Name: "a", Type: "file", ModifiedAt: now}, &FileEntry{}},
		{"file.preview", FilePreviewResult{Entry: FileEntry{Path: "/tmp/a", Name: "a", Type: "file", ModifiedAt: now}, MIMEType: "text/plain", Content: []byte("ok"), Truncated: true}, &FilePreviewResult{}},
		{"file.rename", FileOperationResult{Path: "/tmp/a", TargetPath: "/tmp/b", Success: true}, &FileOperationResult{}},
		{"file.move", FileBatchResult{Results: []FileOperationResult{{Path: "/tmp/a", TargetPath: "/tmp/d/a", Success: true}}}, &FileBatchResult{}},
		{"file.download.open", FileTransferOpenResult{TransferID: "transfer", Channel: 9, Path: "/tmp/a", Offset: 12, Size: 30, ModifiedAt: now, WindowBytes: 1024, ChunkBytes: 256}, &FileTransferOpenResult{}},
		{"file.transfer.cancel", FileTransferCancelResult{Cancelled: true}, &FileTransferCancelResult{}},
	}
	for _, test := range resultCases {
		t.Run(test.method+" result", func(t *testing.T) {
			payload, err := EncodeMethodResult(test.method, test.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := DecodeMethodResult(test.method, payload, test.out); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reflect.ValueOf(test.out).Elem().Interface(), test.value) {
				t.Fatalf("got %#v want %#v", test.out, test.value)
			}
		})
	}
}
