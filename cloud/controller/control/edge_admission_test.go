package control

import "testing"

func TestInvalidateEdgeStopsEveryConnectionGeneration(t *testing.T) {
	first := &connectionGeneration{edgeID: "edge-a", invalidated: make(chan struct{})}
	second := &connectionGeneration{edgeID: "edge-a", invalidated: make(chan struct{})}
	other := &connectionGeneration{edgeID: "edge-b", invalidated: make(chan struct{})}
	service := &Service{
		connections:     map[string]*connectionGeneration{"first": first, "second": second, "other": other},
		edgeConnections: map[string]string{"edge-a": "second", "edge-b": "other"},
	}

	service.InvalidateEdge("edge-a")

	for name, generation := range map[string]*connectionGeneration{"first": first, "second": second} {
		select {
		case <-generation.invalidated:
		default:
			t.Fatalf("%s generation was not invalidated", name)
		}
	}
	select {
	case <-other.invalidated:
		t.Fatal("another Edge generation was invalidated")
	default:
	}
	if len(service.connections) != 1 || service.connections["other"] != other {
		t.Fatalf("remaining connections = %#v", service.connections)
	}
	if len(service.edgeConnections) != 1 || service.edgeConnections["edge-b"] != "other" {
		t.Fatalf("remaining Edge connections = %#v", service.edgeConnections)
	}
}
