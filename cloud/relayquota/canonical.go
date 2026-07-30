// Package relayquota owns the deterministic wire digests shared by Controller and Edge.
package relayquota

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

var deterministic = proto.MarshalOptions{Deterministic: true}

func ReserveRequestDigest(request *cloudv1.RelayReserveRequest) ([]byte, error) {
	if request == nil {
		return nil, errors.New("Relay reserve request is required")
	}
	canonical := proto.Clone(request).(*cloudv1.RelayReserveRequest)
	canonical.RequestDigest = nil
	return digest(canonical)
}

func PolicyDigest(policy *cloudv1.RelayPolicySnapshot) ([]byte, error) {
	if policy == nil {
		return nil, errors.New("Relay policy snapshot is required")
	}
	canonical := proto.Clone(policy).(*cloudv1.RelayPolicySnapshot)
	for index := range canonical.AllowedRegions {
		canonical.AllowedRegions[index] = strings.TrimSpace(canonical.AllowedRegions[index])
	}
	slices.Sort(canonical.AllowedRegions)
	canonical.AllowedRegions = slices.Compact(canonical.AllowedRegions)
	return digest(canonical)
}

func SettlementDigest(settlement *cloudv1.RelaySettlement) ([]byte, error) {
	if settlement == nil {
		return nil, errors.New("Relay settlement is required")
	}
	return digest(settlement)
}

func EqualDigest(left, right []byte) bool {
	return len(left) == sha256.Size && len(right) == sha256.Size && bytes.Equal(left, right)
}

func digest(message proto.Message) ([]byte, error) {
	payload, err := deterministic.Marshal(message)
	if err != nil {
		return nil, err
	}
	value := sha256.Sum256(payload)
	return value[:], nil
}
