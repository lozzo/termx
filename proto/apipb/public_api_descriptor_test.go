package apipb

import (
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestPublicAPIDescriptorBaseline 锁定完整公共 API descriptor。
// 任何字段、enum、oneof、reserved 或 message 变动都必须显式更新 baseline 并接受 schema review。
func TestPublicAPIDescriptorBaseline(t *testing.T) {
	payload, err := os.ReadFile("testdata/public-api-v1.pb")
	if err != nil {
		t.Fatal(err)
	}
	var baseline descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(payload, &baseline); err != nil {
		t.Fatalf("decode descriptor baseline: %v", err)
	}
	if len(baseline.GetFile()) != 3 {
		t.Fatalf("descriptor baseline contains %d files, want 3", len(baseline.GetFile()))
	}
	want := []string{"apipb/common.proto", "apipb/terminal.proto", "apipb/application.proto"}
	for index, name := range want {
		if baseline.GetFile()[index].GetName() != name {
			t.Fatalf("descriptor file[%d]=%q want %q", index, baseline.GetFile()[index].GetName(), name)
		}
	}
	current := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(File_apipb_common_proto),
		protodesc.ToFileDescriptorProto(File_apipb_terminal_proto),
		protodesc.ToFileDescriptorProto(File_apipb_application_proto),
	}}
	if !proto.Equal(&baseline, current) {
		t.Fatal("public API descriptor differs from testdata/public-api-v1.pb; update the baseline only after schema compatibility review")
	}
}
