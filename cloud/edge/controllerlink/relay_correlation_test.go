package controllerlink

import (
	"context"
	"strings"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestRelayResponseIsCorrelatedByReservationID(t *testing.T) {
	session := &Session{done: make(chan struct{}), outbound: make(chan any, 1), waiters: make(map[string]chan any)}
	request := &cloudv1.RelayReserveRequest{ReservationId: "reservation-a"}
	result := make(chan *cloudv1.RelayReserveResponse, 1)
	errors := make(chan error, 1)
	go func() {
		response, err := session.ReserveRelay(context.Background(), request)
		if err != nil {
			errors <- err
			return
		}
		result <- response
	}()
	payload := <-session.outbound
	if payload.(*cloudv1.EdgeEvent_RelayReserve).RelayReserve != request {
		t.Fatal("queued Relay request changed identity")
	}
	session.deliverRelayResponse(waiterKey("reserve", "another-reservation"), &cloudv1.RelayReserveResponse{ReservationId: "another-reservation"})
	want := &cloudv1.RelayReserveResponse{ReservationId: "reservation-a", Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED}
	session.deliverRelayResponse(waiterKey("reserve", "reservation-a"), want)
	select {
	case err := <-errors:
		t.Fatal(err)
	case response := <-result:
		if response != want {
			t.Fatalf("response=%v want=%v", response, want)
		}
	}
}

func TestRelayWaiterFailsWithControlGeneration(t *testing.T) {
	session := &Session{done: make(chan struct{}), outbound: make(chan any, 1), waiters: make(map[string]chan any)}
	errors := make(chan error, 1)
	go func() {
		_, err := session.QueryRelay(context.Background(), &cloudv1.RelayQueryRequest{ReservationId: "uncertain"})
		errors <- err
	}()
	<-session.outbound
	close(session.done)
	if err := <-errors; err == nil || !strings.Contains(err.Error(), "generation closed") {
		t.Fatalf("generation close error=%v", err)
	}
}

func TestRelayWaiterLimitIsBounded(t *testing.T) {
	session := &Session{done: make(chan struct{}), outbound: make(chan any, 1), waiters: make(map[string]chan any)}
	for index := 0; index < maxRelayWaiters; index++ {
		session.waiters[waiterKey("reserve", string(rune(index)))] = make(chan any, 1)
	}
	_, err := session.ReserveRelay(context.Background(), &cloudv1.RelayReserveRequest{ReservationId: "overflow"})
	if err == nil || !strings.Contains(err.Error(), "waiter limit") {
		t.Fatalf("waiter overflow error=%v", err)
	}
}
