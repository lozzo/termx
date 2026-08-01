package runtime

import (
	"context"
	"errors"
	"strings"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

// ApplyDaemonStateSnapshot atomically replaces lifecycle truth for one EdgeControl generation.
func (state *State) ApplyDaemonStateSnapshot(ctx context.Context, snapshot *cloudv1.DaemonStateSnapshot) error {
	states, err := normalizeDaemonStateSnapshot(snapshot)
	if err != nil {
		return err
	}
	var closers []func()
	err = state.mutate(ctx, func(data *stateData) error {
		data.daemonStates = states
		data.daemonStatesReady = true
		for daemonID := range data.agentPresences {
			closers = append(closers, data.reconcileDaemonState(daemonID)...)
		}
		return nil
	})
	runClosers(closers)
	return err
}

// ApplyDaemonStateDelta applies a complete per-daemon replacement from Controller.
func (state *State) ApplyDaemonStateDelta(ctx context.Context, delta *cloudv1.DaemonStateDelta) error {
	if delta == nil {
		return errors.New("daemon state delta is required")
	}
	record, err := normalizeDaemonStateRecord(delta.GetDaemon())
	if err != nil {
		return err
	}
	var closers []func()
	err = state.mutate(ctx, func(data *stateData) error {
		if !data.daemonStatesReady {
			return ErrDaemonStateUnavailable
		}
		current := data.daemonStates[record.GetDaemonId()]
		if current != nil {
			if record.GetStateRevision() < current.GetStateRevision() {
				return nil
			}
			if record.GetStateRevision() == current.GetStateRevision() {
				if proto.Equal(current, record) {
					return nil
				}
				return errors.New("daemon state revision conflicts with cached state")
			}
			if current.GetState() == cloudv1.DaemonState_DAEMON_STATE_DELETED {
				return errors.New("deleted daemon state is terminal")
			}
		}
		data.daemonStates[record.GetDaemonId()] = record
		closers = append(closers, data.reconcileDaemonState(record.GetDaemonId())...)
		return nil
	})
	runClosers(closers)
	return err
}

// InvalidateDaemonStates fails closed when the authoritative Controller stream ends.
func (state *State) InvalidateDaemonStates(ctx context.Context) error {
	var closers []func()
	err := state.mutate(ctx, func(data *stateData) error {
		data.daemonStatesReady = false
		data.daemonStates = make(map[string]*cloudv1.DaemonStateRecord)
		for daemonID, writer := range data.agentWriters {
			closers = append(closers, data.drainDaemonBusiness(daemonID)...)
			if writer.close != nil {
				closers = append(closers, writer.close)
			}
		}
		return nil
	})
	runClosers(closers)
	return err
}

func (state *State) DaemonState(ctx context.Context, daemonID string) (*cloudv1.DaemonStateRecord, error) {
	var result *cloudv1.DaemonStateRecord
	err := state.call(ctx, func(data *stateData) error {
		record, err := data.requireDaemonState(daemonID)
		if err != nil {
			return err
		}
		result = proto.Clone(record).(*cloudv1.DaemonStateRecord)
		return nil
	})
	return result, err
}

// ApplyDaemonLifecycleResult publishes ACTIVE Presence only after daemon peers converged.
func (state *State) ApplyDaemonLifecycleResult(ctx context.Context, daemonID string, generation uint64, result *cloudv1.DaemonLifecycleResult) error {
	var closeWriter func()
	err := state.mutate(ctx, func(data *stateData) error {
		current := data.daemonStates[daemonID]
		presence := data.agentPresences[daemonID]
		writer := data.agentWriters[daemonID]
		if !data.daemonStatesReady || current == nil || presence == nil || writer.generation != generation || presence.GetGeneration() != generation || result == nil || result.GetAgentGeneration() != generation || result.GetDaemonState() == nil || !proto.Equal(current, result.GetDaemonState()) {
			return ErrStaleGeneration
		}
		if !result.GetApplied() {
			closeWriter = writer.close
			return nil
		}
		switch current.GetState() {
		case cloudv1.DaemonState_DAEMON_STATE_ACTIVE:
			data.publishBusinessAgent(presence)
		case cloudv1.DaemonState_DAEMON_STATE_BLOCKED:
			data.removeBusinessAgent(daemonID)
		case cloudv1.DaemonState_DAEMON_STATE_DELETED:
			data.removeBusinessAgent(daemonID)
			closeWriter = writer.close
		default:
			return ErrDaemonStateUnavailable
		}
		return nil
	})
	if closeWriter != nil {
		closeWriter()
	}
	return err
}

func (data *stateData) reconcileDaemonState(daemonID string) []func() {
	policy := data.daemonStates[daemonID]
	presence := data.agentPresences[daemonID]
	writer := data.agentWriters[daemonID]
	if policy == nil || presence == nil || writer.send == nil || writer.generation != presence.GetGeneration() {
		return data.drainDaemonBusiness(daemonID)
	}
	closers := []func(){}
	if policy.GetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE {
		closers = append(closers, data.drainDaemonBusiness(daemonID)...)
	} else {
		data.removeBusinessAgent(daemonID)
	}
	command := &cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Lifecycle{Lifecycle: &cloudv1.DaemonLifecycleCommand{DaemonState: proto.Clone(policy).(*cloudv1.DaemonStateRecord), AgentGeneration: presence.GetGeneration()}}}
	if !writer.send(command) && writer.close != nil {
		closers = append(closers, writer.close)
	}
	return closers
}

func (data *stateData) drainDaemonBusiness(daemonID string) []func() {
	data.removeBusinessAgent(daemonID)
	if presence := data.agentPresences[daemonID]; presence != nil {
		data.cancelAgentSignals(daemonID, presence.GetGeneration())
	}
	closers := make([]func(), 0)
	for sessionID, session := range data.sessions {
		if session.GetDaemonId() != daemonID {
			continue
		}
		if closer := data.sessionClosers[sessionID]; closer.close != nil {
			closers = append(closers, closer.close)
		}
		delete(data.sessions, sessionID)
		delete(data.sessionClosers, sessionID)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_SessionRemoved{SessionRemoved: &cloudv1.ClientSessionRemoved{SessionId: sessionID, Generation: session.GetGeneration()}}})
	}
	for _, group := range data.relayGroups {
		if group.grant.GetPolicy().GetDaemonId() == daemonID {
			group.closing = true
			delete(data.relayAuth, group.username)
		}
	}
	return closers
}

func (data *stateData) publishBusinessAgent(presence *cloudv1.AgentPresence) {
	if presence == nil || proto.Equal(data.agents[presence.GetDaemonId()], presence) {
		return
	}
	clone := proto.Clone(presence).(*cloudv1.AgentPresence)
	data.agents[clone.GetDaemonId()] = clone
	data.revision++
	data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentUpserted{AgentUpserted: proto.Clone(clone).(*cloudv1.AgentPresence)}})
}

func (data *stateData) removeBusinessAgent(daemonID string) {
	current := data.agents[daemonID]
	if current == nil {
		return
	}
	delete(data.agents, daemonID)
	data.revision++
	data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentRemoved{AgentRemoved: &cloudv1.AgentRemoved{DaemonId: daemonID, Generation: current.GetGeneration()}}})
}

func (data *stateData) requireDaemonState(daemonID string) (*cloudv1.DaemonStateRecord, error) {
	if !data.daemonStatesReady {
		return nil, ErrDaemonStateUnavailable
	}
	record := data.daemonStates[strings.TrimSpace(daemonID)]
	if record == nil {
		return nil, ErrDaemonStateUnavailable
	}
	return record, nil
}

func (data *stateData) requireActiveDaemon(daemonID string) (*cloudv1.DaemonStateRecord, error) {
	record, err := data.requireDaemonState(daemonID)
	if err != nil {
		return nil, err
	}
	switch record.GetState() {
	case cloudv1.DaemonState_DAEMON_STATE_ACTIVE:
		return record, nil
	case cloudv1.DaemonState_DAEMON_STATE_BLOCKED:
		return nil, ErrDaemonBlocked
	case cloudv1.DaemonState_DAEMON_STATE_DELETED:
		return nil, ErrDaemonDeleted
	default:
		return nil, ErrDaemonStateUnavailable
	}
}

func normalizeDaemonStateSnapshot(snapshot *cloudv1.DaemonStateSnapshot) (map[string]*cloudv1.DaemonStateRecord, error) {
	if snapshot == nil {
		return nil, errors.New("daemon state snapshot is required")
	}
	states := make(map[string]*cloudv1.DaemonStateRecord, len(snapshot.GetDaemons()))
	for _, input := range snapshot.GetDaemons() {
		record, err := normalizeDaemonStateRecord(input)
		if err != nil {
			return nil, err
		}
		if _, exists := states[record.GetDaemonId()]; exists {
			return nil, errors.New("daemon state snapshot contains duplicate identity")
		}
		states[record.GetDaemonId()] = record
	}
	return states, nil
}

func normalizeDaemonStateRecord(input *cloudv1.DaemonStateRecord) (*cloudv1.DaemonStateRecord, error) {
	if input == nil || strings.TrimSpace(input.GetDaemonId()) == "" || input.GetStateRevision() == 0 ||
		(input.GetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE && input.GetState() != cloudv1.DaemonState_DAEMON_STATE_BLOCKED && input.GetState() != cloudv1.DaemonState_DAEMON_STATE_DELETED) {
		return nil, errors.New("daemon state record is invalid")
	}
	clone := proto.Clone(input).(*cloudv1.DaemonStateRecord)
	clone.DaemonId = strings.TrimSpace(clone.GetDaemonId())
	return clone, nil
}

func runClosers(closers []func()) {
	for _, closeOwned := range closers {
		if closeOwned != nil {
			closeOwned()
		}
	}
}
