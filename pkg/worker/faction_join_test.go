package worker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type joinFake struct {
	*fakeClient
	accepted []string
	err      error
}

func (f *joinFake) FactionAcceptInvite(_ context.Context, id string) error {
	f.accepted = append(f.accepted, id)
	return f.err
}

// New route-fleet truckers (2026-11-20) join CRFT from a scheduled task: a
// play_as login would kick the running worker's session.
func TestFactionAcceptInviteVerb(t *testing.T) {
	fc := &joinFake{fakeClient: &fakeClient{state: &game.State{}}}
	var out bytes.Buffer
	d := NewWorkerDispatch(fc, nil, nil, &out)
	if err := d.Run(context.Background(), []string{"faction_accept_invite", "e727c0e918d994c72db2978fe5b18edc"}); err != nil {
		t.Fatal(err)
	}
	if len(fc.accepted) != 1 || fc.accepted[0] != "e727c0e918d994c72db2978fe5b18edc" {
		t.Fatalf("accepted %v", fc.accepted)
	}
	if err := d.Run(context.Background(), []string{"faction_accept_invite"}); err == nil {
		t.Fatal("missing FACTION_ID accepted")
	}
}

// Once joined, the invite is gone; the hourly task must not fail forever.
func TestFactionAcceptInviteAlreadyMemberIsNoop(t *testing.T) {
	fc := &joinFake{fakeClient: &fakeClient{state: &game.State{}}, err: errors.New("already_in_faction: You are already in a faction")}
	var out bytes.Buffer
	d := NewWorkerDispatch(fc, nil, nil, &out)
	if err := d.Run(context.Background(), []string{"faction_accept_invite", "e727"}); err != nil {
		t.Fatalf("already a member should be a no-op, got %v", err)
	}
	if !strings.Contains(out.String(), "already") {
		t.Errorf("log %q", out.String())
	}
}
