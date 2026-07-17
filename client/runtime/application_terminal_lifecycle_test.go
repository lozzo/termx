package runtime

import (
	"context"
	"reflect"
	"testing"

	coreapi "github.com/lozzow/termx/core/api"
)

func TestTerminalRefRequiresOwningEndpoint(t *testing.T) {
	for _, ref := range []TerminalRef{{EndpointID: "local"}, {TerminalID: "term-1"}, {}} {
		if err := ref.Validate(); CodeOf(err) != ErrorInvalidRequest {
			t.Fatalf("ref %#v error=%v code=%q", ref, err, CodeOf(err))
		}
	}
	if err := (TerminalRef{EndpointID: "studio", TerminalID: "term-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

type contractTerminalLifecycleApplication struct{}

func (contractTerminalLifecycleApplication) TerminalDefaults(context.Context, TerminalDefaultsRequest) (TerminalDefaultsResult, error) {
	return TerminalDefaultsResult{}, nil
}

func (contractTerminalLifecycleApplication) CreateTerminal(context.Context, TerminalCreateRequest) (TerminalCreateResult, error) {
	return TerminalCreateResult{}, nil
}

func (contractTerminalLifecycleApplication) ListTerminals(context.Context, TerminalListRequest) (TerminalListResult, error) {
	return TerminalListResult{}, nil
}

func (contractTerminalLifecycleApplication) RestartTerminal(context.Context, TerminalMutationRequest) error {
	return nil
}

func (contractTerminalLifecycleApplication) KillTerminal(context.Context, TerminalMutationRequest) error {
	return nil
}

func (contractTerminalLifecycleApplication) RemoveTerminal(context.Context, TerminalMutationRequest) error {
	return nil
}

func (contractTerminalLifecycleApplication) SetTerminalMetadata(context.Context, TerminalMetadataRequest) error {
	return nil
}

func (contractTerminalLifecycleApplication) SetTerminalTags(context.Context, TerminalTagsRequest) error {
	return nil
}

var _ TerminalLifecycleApplication = contractTerminalLifecycleApplication{}

func TestTerminalApplicationContractWrapsCoreProjection(t *testing.T) {
	assertFieldType(t, TerminalDefaultsResult{}, "Defaults", reflect.TypeOf(coreapi.PathDefaults{}))
	assertFieldType(t, TerminalCreateRequest{}, "Spec", reflect.TypeOf(coreapi.TerminalCreateSpec{}))
	assertFieldType(t, TerminalCreateResult{}, "Terminal", reflect.TypeOf(coreapi.TerminalCreateResult{}))
	assertFieldType(t, TerminalListResult{}, "Items", reflect.TypeOf([]coreapi.TerminalInfo{}))
	assertFieldType(t, TerminalMetadataRequest{}, "Update", reflect.TypeOf(coreapi.TerminalMetadataUpdate{}))
	assertFieldType(t, TerminalTagsRequest{}, "Update", reflect.TypeOf(coreapi.TerminalTagsUpdate{}))
}

func assertFieldType(t *testing.T, value any, fieldName string, want reflect.Type) {
	t.Helper()
	field, ok := reflect.TypeOf(value).FieldByName(fieldName)
	if !ok {
		t.Fatalf("%T is missing field %s", value, fieldName)
	}
	if field.Type != want {
		t.Fatalf("%T.%s type=%s want=%s", value, fieldName, field.Type, want)
	}
}
