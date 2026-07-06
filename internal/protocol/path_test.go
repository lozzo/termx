package protocol

import (
	"reflect"
	"testing"
)

func TestPathListDirsProtocolRoundTrip(t *testing.T) {
	params := PathListDirsParams{Prefix: "~/pro", Limit: 20}
	payload, err := EncodeMethodParams("path.list_dirs", params)
	if err != nil {
		t.Fatalf("encode path list dirs params: %v", err)
	}
	decoded, err := DecodeMethodParams("path.list_dirs", payload)
	if err != nil {
		t.Fatalf("decode path list dirs params: %v", err)
	}
	if !reflect.DeepEqual(decoded, params) {
		t.Fatalf("path params mismatch:\n got: %#v\nwant: %#v", decoded, params)
	}

	result := PathListDirsResult{
		BasePath:  "/home/root",
		Missing:   false,
		Truncated: true,
		Entries: []PathDirEntry{
			{Name: "projects", Path: "~/projects/"},
			{Name: "profiles", Path: "~/profiles/"},
		},
	}
	resultPayload, err := EncodeMethodResult("path.list_dirs", &result)
	if err != nil {
		t.Fatalf("encode path list dirs result: %v", err)
	}
	var got PathListDirsResult
	if err := DecodeMethodResult("path.list_dirs", resultPayload, &got); err != nil {
		t.Fatalf("decode path list dirs result: %v", err)
	}
	if !reflect.DeepEqual(got, result) {
		t.Fatalf("path result mismatch:\n got: %#v\nwant: %#v", got, result)
	}

	var wrongTarget struct{ BasePath string }
	if err := DecodeMethodResult("path.list_dirs", resultPayload, &wrongTarget); err == nil {
		t.Fatal("expected path result decode to reject arbitrary struct target")
	}
}
