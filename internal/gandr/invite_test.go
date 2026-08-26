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
	// The decoded credentials must actually unlock the group, exactly like
	// /grupp öppna <inbjudan> relies on (UnlockPrivateGroup re-verifies the
	// password against the stored wrapped key rather than trusting it).
	if err := session.UnlockPrivateGroup(id, password); err != nil {
		t.Fatalf("den avkodade inbjudan låste inte upp gruppen: %v", err)
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
