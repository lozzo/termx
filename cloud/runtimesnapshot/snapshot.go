// Package runtimesnapshot 提供 Edge 与 Controller 共用的运行时快照规范化和摘要校验。
// 它不持有在线状态，只保证同一 Proto 投影在两端得到相同的确定性摘要。
package runtimesnapshot

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

// NormalizeClone 深拷贝并按稳定主键排序快照，避免 map 迭代顺序进入 wire digest。
func NormalizeClone(snapshot *cloudv1.RuntimeSnapshot) (*cloudv1.RuntimeSnapshot, error) {
	if snapshot == nil {
		return nil, errors.New("runtime snapshot is required")
	}
	clone := proto.Clone(snapshot).(*cloudv1.RuntimeSnapshot)
	slices.SortFunc(clone.Agents, func(left, right *cloudv1.AgentPresence) int {
		return strings.Compare(left.GetDaemonId(), right.GetDaemonId())
	})
	slices.SortFunc(clone.Sessions, func(left, right *cloudv1.ClientSessionSummary) int {
		return strings.Compare(left.GetSessionId(), right.GetSessionId())
	})
	seenAgents := make(map[string]struct{}, len(clone.Agents))
	for _, agent := range clone.Agents {
		if err := validateAgent(agent); err != nil {
			return nil, err
		}
		if _, exists := seenAgents[agent.GetDaemonId()]; exists {
			return nil, fmt.Errorf("duplicate daemon %q", agent.GetDaemonId())
		}
		seenAgents[agent.GetDaemonId()] = struct{}{}
	}
	seenSessions := make(map[string]struct{}, len(clone.Sessions))
	for _, session := range clone.Sessions {
		if err := validateSession(session); err != nil {
			return nil, err
		}
		if _, exists := seenSessions[session.GetSessionId()]; exists {
			return nil, fmt.Errorf("duplicate session %q", session.GetSessionId())
		}
		seenSessions[session.GetSessionId()] = struct{}{}
	}
	return clone, nil
}

// Digest 返回规范化快照的 SHA-256；revision 也进入摘要，禁止跨 revision 复用内容。
func Digest(snapshot *cloudv1.RuntimeSnapshot) ([]byte, error) {
	normalized, err := NormalizeClone(snapshot)
	if err != nil {
		return nil, err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

// ValidateDelta 校验增量的对象身份与 generation；revision 连续性由状态 owner 校验。
func ValidateDelta(delta *cloudv1.RuntimeDelta) error {
	if delta == nil || delta.GetRevision() == 0 {
		return errors.New("runtime delta and positive revision are required")
	}
	switch change := delta.GetChange().(type) {
	case *cloudv1.RuntimeDelta_AgentUpserted:
		return validateAgent(change.AgentUpserted)
	case *cloudv1.RuntimeDelta_AgentRemoved:
		if change.AgentRemoved == nil || strings.TrimSpace(change.AgentRemoved.GetDaemonId()) == "" || change.AgentRemoved.GetGeneration() == 0 {
			return errors.New("removed agent identity and generation are required")
		}
	case *cloudv1.RuntimeDelta_SessionUpserted:
		return validateSession(change.SessionUpserted)
	case *cloudv1.RuntimeDelta_SessionRemoved:
		if change.SessionRemoved == nil || strings.TrimSpace(change.SessionRemoved.GetSessionId()) == "" || change.SessionRemoved.GetGeneration() == 0 {
			return errors.New("removed session identity and generation are required")
		}
	default:
		return errors.New("runtime delta change is required")
	}
	return nil
}

func validateAgent(agent *cloudv1.AgentPresence) error {
	if agent == nil || strings.TrimSpace(agent.GetDaemonId()) == "" || strings.TrimSpace(agent.GetBootId()) == "" || strings.TrimSpace(agent.GetConnectionId()) == "" || agent.GetGeneration() == 0 {
		return errors.New("agent daemon, boot, connection, and generation are required")
	}
	if agent.GetTicketIssuedAt() == nil || agent.GetTicketIssuedAt().CheckValid() != nil {
		return errors.New("agent ticket_issued_at is invalid")
	}
	return nil
}

func validateSession(session *cloudv1.ClientSessionSummary) error {
	if session == nil || strings.TrimSpace(session.GetSessionId()) == "" || strings.TrimSpace(session.GetDaemonId()) == "" || strings.TrimSpace(session.GetClientId()) == "" || session.GetGeneration() == 0 {
		return errors.New("session, daemon, client, and generation are required")
	}
	return nil
}
