package sessionflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
)

func TestAnswerLocalAndManagedUseSharedOrchestrator(t *testing.T) {
	iceServers := []hubv1.RTCIceServerConfig{{URLs: []string{"stun:stun.example.test:3478"}}}
	answerer := &recordingAnswerer{answer: hubv1.SignalingAnswer{SessionID: "answer-session"}}

	answer, err := AnswerLocal(context.Background(), answerer, AnswerInput{
		Plan:  LocalPlan(iceServers),
		Offer: hubv1.SignalingOffer{SessionID: "local-session"},
	})
	if err != nil {
		t.Fatalf("AnswerLocal returned error: %v", err)
	}
	if answer.SessionID != "answer-session" {
		t.Fatalf("local answer session = %q", answer.SessionID)
	}
	if !reflect.DeepEqual(answerer.iceServers, iceServers) {
		t.Fatalf("local orchestrator did not pass ICE servers: %+v", answerer.iceServers)
	}

	answerer = &recordingAnswerer{answer: hubv1.SignalingAnswer{SessionID: "managed-answer"}}
	answer, err = AnswerManaged(context.Background(), answerer, AnswerInput{
		Plan:  ManagedPlan(iceServers, RelayPolicy{AllowRelay: true}),
		Offer: hubv1.SignalingOffer{SessionID: "managed-session"},
	})
	if err != nil {
		t.Fatalf("AnswerManaged returned error: %v", err)
	}
	if answer.SessionID != "managed-answer" || answerer.offer.SessionID != "managed-session" {
		t.Fatalf("managed orchestrator did not delegate offer/answer: answer=%+v offer=%+v", answer, answerer.offer)
	}
}

func TestAnswerOrchestratorRejectsRelayAsClientPath(t *testing.T) {
	_, err := AnswerManaged(context.Background(), &recordingAnswerer{}, AnswerInput{
		Plan:  Plan{Path: "relay"},
		Offer: hubv1.SignalingOffer{SessionID: "session_1"},
	})
	if err == nil || !strings.Contains(err.Error(), "relay is not a client path") {
		t.Fatalf("expected relay client path rejection, got %v", err)
	}
}

type recordingAnswerer struct {
	answer     hubv1.SignalingAnswer
	err        error
	offer      hubv1.SignalingOffer
	iceServers []hubv1.RTCIceServerConfig
}

func (a *recordingAnswerer) AnswerOffer(
	_ context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	_ bridge.TransportSink,
	_ *fileapi.Manager,
	_ remotertc.AnswerOptions,
) (hubv1.SignalingAnswer, error) {
	a.offer = offer
	a.iceServers = append([]hubv1.RTCIceServerConfig(nil), iceServers...)
	if a.err != nil {
		return hubv1.SignalingAnswer{}, a.err
	}
	if a.answer.SessionID == "" {
		return hubv1.SignalingAnswer{}, errors.New("test answerer requires answer session")
	}
	return a.answer, nil
}
