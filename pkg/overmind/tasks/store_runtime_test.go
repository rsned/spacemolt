package tasks

import (
	"io"
	"log"
	"testing"
)

func newTestStore() *Store {
	return NewStore(nil, log.New(io.Discard, "", 0))
}

func TestAddValidatesAndRejectsDuplicates(t *testing.T) {
	s := newTestStore()
	good := Task{ID: "p1/craft-1", Script: "craft_node", RoleRequired: "craftsman"}
	if err := s.Add(good); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.Add(good); err == nil {
		t.Fatal("duplicate id accepted")
	}
	for _, bad := range []Task{
		{ID: "", Script: "x", RoleRequired: "r"},
		{ID: "a:b", Script: "x", RoleRequired: "r"},
		{ID: "b", Script: "", RoleRequired: "r"},
		{ID: "c", Script: "x", RoleRequired: ""},
	} {
		if err := s.Add(bad); err == nil {
			t.Errorf("invalid task accepted: %+v", bad)
		}
	}
	got, ok := s.Get("p1/craft-1")
	if !ok || got.Status != StatusPending {
		t.Fatalf("get = %+v, %v; want pending task", got, ok)
	}
}

func TestRemove(t *testing.T) {
	s := newTestStore()
	_ = s.Add(Task{ID: "p1/n1", Script: "deliver_node", RoleRequired: "craftsman"})
	if !s.Remove("p1/n1") {
		t.Fatal("remove: not found")
	}
	if s.Remove("p1/n1") {
		t.Fatal("second remove reported found")
	}
	if _, ok := s.Get("p1/n1"); ok {
		t.Fatal("removed task still present")
	}
}
