package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

type fakeEnrollmentRegistry struct {
	deployments []hubregistry.Deployment
	assignments map[string][]hubregistry.Assignment
}

func (registry fakeEnrollmentRegistry) Deployments(context.Context) ([]hubregistry.Deployment, error) {
	return registry.deployments, nil
}

func (registry fakeEnrollmentRegistry) AssignmentsForHub(_ context.Context, hubID string, _ time.Time) ([]hubregistry.Assignment, error) {
	return registry.assignments[hubID], nil
}

type fakeEnrollmentAttachments map[string]bool

func (attachments fakeEnrollmentAttachments) AttachmentStatus(hubID string) (uint64, time.Time, bool) {
	return 1, time.Now(), attachments[hubID]
}

func TestEnrollmentCandidateProviderFiltersInactiveDisabledAndFullHubs(t *testing.T) {
	deployments := []hubregistry.Deployment{
		enrollmentDeployment("hub-active", 2),
		enrollmentDeployment("hub-inactive", 2),
		enrollmentDeployment("hub-disabled", 2),
		enrollmentDeployment("hub-full", 1),
	}
	registry := fakeEnrollmentRegistry{assignments: map[string][]hubregistry.Assignment{
		"hub-active": {{Value: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1"}}},
		"hub-full":   {{Value: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-2"}}},
	}}
	for _, deployment := range deployments {
		deployment.Enabled = deployment.Metadata.GetHubId() != "hub-disabled"
		registry.deployments = append(registry.deployments, deployment)
	}
	provider := enrollmentCandidateProvider(registry, fakeEnrollmentAttachments{"hub-active": true, "hub-disabled": true, "hub-full": true})
	candidates, err := provider(context.Background(), time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].value.GetHubId() != "hub-active" || candidates[0].assignmentCount != 1 {
		t.Fatalf("filtered enrollment candidates = %v", candidates)
	}
}

func TestEnrollmentCandidateProviderKeepsFullExistingHubOnlyForOwner(t *testing.T) {
	deployment := enrollmentDeployment("hub-full", 1)
	registry := fakeEnrollmentRegistry{
		deployments: []hubregistry.Deployment{deployment},
		assignments: map[string][]hubregistry.Assignment{"hub-full": {{Value: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-owner"}}}},
	}
	provider := enrollmentCandidateProvider(registry, fakeEnrollmentAttachments{"hub-full": true})
	if candidates, err := provider(context.Background(), time.Now(), ""); err != nil || len(candidates) != 0 {
		t.Fatalf("new daemon full Hub candidates = (%v, %v)", candidates, err)
	}
	candidates, err := provider(context.Background(), time.Now(), "hub-full")
	if err != nil || len(candidates) != 1 || candidates[0].value.GetHubId() != "hub-full" || candidates[0].assignmentCount != 1 {
		t.Fatalf("existing daemon full Hub candidates = (%v, %v)", candidates, err)
	}
}

func TestEnrollmentCandidateProviderCapsStableCandidateListAtOneHundred(t *testing.T) {
	registry := fakeEnrollmentRegistry{assignments: map[string][]hubregistry.Assignment{}}
	attachments := fakeEnrollmentAttachments{}
	for index := range 105 {
		deployment := enrollmentDeployment(fmt.Sprintf("hub-%03d", index), 10)
		registry.deployments = append(registry.deployments, deployment)
		attachments[deployment.Metadata.GetHubId()] = true
	}
	provider := enrollmentCandidateProvider(registry, attachments)
	candidates, err := provider(context.Background(), time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 100 || candidates[0].value.GetHubId() != "hub-000" || candidates[99].value.GetHubId() != "hub-099" {
		t.Fatalf("bounded candidates = first=%v last=%v count=%d", candidates[0], candidates[len(candidates)-1], len(candidates))
	}
}

func enrollmentDeployment(hubID string, maximum uint64) hubregistry.Deployment {
	return hubregistry.Deployment{Metadata: &cloudpb.EdgeDeploymentMetadata{HubId: hubID, Region: "test-1"}, PublicHubURL: "https://" + hubID + ".example.test", HealthURL: "https://" + hubID + ".example.test/healthz", MaxAssignments: maximum, IdentityApproved: true, Enabled: true, DirectoryRevision: 1}
}
