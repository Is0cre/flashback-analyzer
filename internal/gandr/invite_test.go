package gandr

import (
	"strings"
	"testing"
)

func TestGroupInviteRoundTrip(t *testing.T) {
	subsystem := NewAt(t.TempDir() + "/identity.key")
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	group, err := session.CreatePrivateGroup("Hemlig grupp", "grupp-losenord")
	if err != nil {
		t.Fatal(err)
	}

	invite, err := EncodeGroupInvite(group.ID, group.Name, "grupp-losenord")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(invite, "BFG1.") {
		t.Fatalf("oväntat inbjudningsformat: %q", invite)
	}
	if !IsGroupInvite(invite) {
		t.Fatal("IsGroupInvite kände inte igen en giltig inbjudan")
	}

	id, name, password, err := DecodeGroupInvite(invite)
	if err != nil {
		t.Fatal(err)
	}
	if id != group.ID || name != group.Name || password != "grupp-losenord" {
		t.Fatalf("inbjudan avkodades fel: id=%x namn=%q lösenord=%q", id, name, password)
	}

	// The actual point of this test: a completely separate person —
	// different identity, different device, different local DB, never
	// having created or opened this group before — can join using only
	// what the invite carried. An earlier design couldn't do this at
	// all: the group's real key was random and only ever existed
	// wrapped inside the creator's own local DB, so a correct password
	// still wasn't enough for anyone else to reach the same key.
	memberSubsystem := NewAt(t.TempDir() + "/member/identity.key")
	if err := memberSubsystem.Create("ett helt annat valvlösenord"); err != nil {
		t.Fatal(err)
	}
	memberSession, err := memberSubsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer memberSession.Close()

	if err := memberSession.UnlockPrivateGroup(id, password, name); err != nil {
		t.Fatalf("en ny medlem kunde inte gå med i gruppen via inbjudan: %v", err)
	}
	groups, err := memberSession.PrivateGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != id || groups[0].Name != name {
		t.Fatalf("gruppen bokmärktes inte korrekt lokalt hos den nya medlemmen: %#v", groups)
	}
}

func TestDecodeGroupInviteRejectsGarbage(t *testing.T) {
	if _, _, _, err := DecodeGroupInvite("not-an-invite"); err == nil {
		t.Fatal("ogiltig inbjudan accepterades")
	}
	if IsGroupInvite("not-an-invite") {
		t.Fatal("IsGroupInvite kände igen en ogiltig sträng")
	}
}
