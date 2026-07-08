package release

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewStore(fake.NewSimpleClientset())
	const ns = "minato-app-demo"

	if err := s.Init(ctx, ns); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Init must be idempotent (app re-creation paths call it defensively).
	if err := s.Init(ctx, ns); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	v1, err := s.Allocate(ctx, ns, "reg/app:v1", "abc1234", "deploy abc1234", StatusBuilding)
	if err != nil {
		t.Fatalf("Allocate v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("first version = %d, want 1", v1.Version)
	}
	if err := s.SetStatus(ctx, ns, v1.Version, StatusLive); err != nil {
		t.Fatalf("SetStatus v1 live: %v", err)
	}

	v2, err := s.Allocate(ctx, ns, "reg/app:v2", "def5678", "deploy def5678", StatusBuilding)
	if err != nil {
		t.Fatalf("Allocate v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("second version = %d, want 2", v2.Version)
	}
	if err := s.SetStatus(ctx, ns, v2.Version, StatusLive); err != nil {
		t.Fatalf("SetStatus v2 live: %v", err)
	}

	h, err := s.Load(ctx, ns)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Marking v2 live must supersede v1: exactly one live release at a time.
	if live := h.Live(); live == nil || live.Version != 2 {
		t.Fatalf("Live() = %+v, want v2", live)
	}
	if prev := h.Previous(); prev == nil || prev.Version != 1 {
		t.Fatalf("Previous() = %+v, want v1", prev)
	}

	// A failed release never becomes the rollback target.
	v3, _ := s.Allocate(ctx, ns, "reg/app:v3", "0000000", "broken build", StatusBuilding)
	if err := s.SetStatus(ctx, ns, v3.Version, StatusFailed); err != nil {
		t.Fatalf("SetStatus v3 failed: %v", err)
	}
	h, _ = s.Load(ctx, ns)
	if live := h.Live(); live == nil || live.Version != 2 {
		t.Fatalf("Live() after failed build = %+v, want v2", live)
	}
	if prev := h.Previous(); prev == nil || prev.Version != 1 {
		t.Fatalf("Previous() after failed build = %+v, want v1", prev)
	}
}
