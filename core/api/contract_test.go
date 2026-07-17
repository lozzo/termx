package api

import (
	"reflect"
	"strings"
	"testing"
)

func TestDaemonProjectionDoesNotOwnClientOrUIIdentity(t *testing.T) {
	for _, value := range []any{
		TerminalInfo{},
		TerminalCreateSpec{},
		TerminalCreateResult{},
		TerminalAttachSpec{},
		TerminalAttachResult{},
		TerminalDetachSpec{},
		TerminalInputSpec{},
		TerminalResizeSpec{},
		TerminalResizeResult{},
		PathDefaults{},
		PathDirectoryQuery{},
		PathDirectoryResult{},
	} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := strings.ToLower(typeOf.Field(index).Name)
			for _, forbidden := range []string{"endpointid", "generation", "routeid", "paneid", "workspaceid", "transport", "protocol"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s exposes non-daemon owner field %s", typeOf.Name(), typeOf.Field(index).Name)
				}
			}
		}
	}
}
