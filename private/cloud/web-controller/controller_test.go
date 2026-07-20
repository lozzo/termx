package webcontroller_test

import (
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

type deviceWriter struct{ devices []domain.DeviceRegistration }

func (writer *deviceWriter) RegisterDevice(device domain.DeviceRegistration) error {
	writer.devices = append(writer.devices, device)
	return nil
}

type entitlementWriter struct{ values []entitlement.Entitlement }

func (writer *entitlementWriter) Put(value entitlement.Entitlement) error {
	writer.values = append(writer.values, value)
	return nil
}

type auditWriter struct {
	pairings []domain.PairingApproval
	events   []domain.AuditEvent
}

func (writer *auditWriter) RecordPairing(approval domain.PairingApproval) error {
	writer.pairings = append(writer.pairings, approval)
	return nil
}

func (writer *auditWriter) Append(event domain.AuditEvent) error {
	writer.events = append(writer.events, event)
	return nil
}

type usageReader struct{ value usage.SessionUsage }

func (reader usageReader) Aggregate(managedSessionID, routeID string) usage.SessionUsage {
	value := reader.value
	value.ManagedSessionID = managedSessionID
	value.RouteID = routeID
	return value
}

func TestControllerIsThinAuditedManagementFacade(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	devices := &deviceWriter{}
	entitlements := &entitlementWriter{}
	auditLog := &auditWriter{}
	controller, err := webcontroller.New(devices, entitlements, auditLog, usageReader{value: usage.SessionUsage{BytesUp: 10, BytesDown: 20}})
	if err != nil {
		t.Fatal(err)
	}
	device := domain.DeviceRegistration{ID: "device", AccountID: "account", OwnerUserID: "user", Kind: domain.DeviceKindDaemon, RegisteredAt: now}
	if err := controller.RegisterDevice("admin", device, "audit-1", now); err != nil {
		t.Fatal(err)
	}
	value := entitlement.Entitlement{AccountID: "account", Status: entitlement.StatusExpired, EffectiveUntil: now, UpdatedAt: now}
	if err := controller.SetEntitlement("admin", value, "audit-2", now); err != nil {
		t.Fatal(err)
	}
	if len(devices.devices) != 1 || len(entitlements.values) != 1 || len(auditLog.events) != 2 {
		t.Fatalf("domain writes devices=%d entitlements=%d audit=%d", len(devices.devices), len(entitlements.values), len(auditLog.events))
	}
	if auditLog.events[1].Action != "entitlement.update" {
		t.Fatalf("entitlement audit action = %q", auditLog.events[1].Action)
	}
	got := controller.RelayUsage("managed", "route")
	if got.BytesUp != 10 || got.BytesDown != 20 || got.ManagedSessionID != "managed" || got.RouteID != "route" {
		t.Fatalf("usage = %#v", got)
	}
}
