package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestEndpointAndAccessContractsDoNotExposeConnectionOrCredentialOwners(t *testing.T) {
	for _, value := range []any{EndpointProbeResult{}, AccessIdentityResult{}, AccessRecord{}, AccessListResult{}} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := strings.ToLower(typeOf.Field(index).Name)
			for _, forbidden := range []string{"transport", "protocol", "capabilitygrant", "privatekey", "credential"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s exposes forbidden field %s", typeOf.Name(), typeOf.Field(index).Name)
				}
			}
		}
	}
}

type contractEndpointApplication struct{}

func (contractEndpointApplication) ProbeEndpoint(context.Context, EndpointProbeRequest) (EndpointProbeResult, error) {
	return EndpointProbeResult{}, nil
}

type contractAccessApplication struct{}

func (contractAccessApplication) AccessIdentity(context.Context, AccessIdentityRequest) (AccessIdentityResult, error) {
	return AccessIdentityResult{}, nil
}

func (contractAccessApplication) ListAccess(context.Context, AccessListRequest) (AccessListResult, error) {
	return AccessListResult{}, nil
}

func (contractAccessApplication) RevokeAccess(context.Context, AccessRevokeRequest) (AccessRecord, error) {
	return AccessRecord{}, nil
}

var _ EndpointApplication = contractEndpointApplication{}
var _ AccessApplication = contractAccessApplication{}
