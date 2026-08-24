package gandr

import (
	"strings"
	"testing"
)

func TestInvitationAddsVerifiedLocalContact(t *testing.T) {
	a := NewAt(t.TempDir() + "/a/identity.key")
	if err := a.Create("a-password"); err != nil {
		t.Fatal(err)
	}
	aSession, err := a.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer aSession.Close()
	token, err := aSession.CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "BFI1.") {
		t.Fatalf("oväntat invitationsformat: %q", token)
	}

	b := NewAt(t.TempDir() + "/b/identity.key")
	if err := b.Create("b-password"); err != nil {
		t.Fatal(err)
	}
	bSession, err := b.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer bSession.Close()
	if _, err := bSession.AcceptInvitation(token, "gröna katten"); err != nil {
		t.Fatal(err)
	}
	contacts, err := bSession.Contacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].Name != "gröna katten" {
		t.Fatalf("kontakt sparades inte lokalt: %#v", contacts)
	}
}

func TestInvitationRejectsTampering(t *testing.T) {
	subsystem := NewAt(t.TempDir() + "/identity.key")
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	token, err := session.CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	token = token[:len(token)-1] + "x"
	if _, err := session.AcceptInvitation(token, "fel"); err == nil {
		t.Fatal("förvanskad invitation accepterades")
	}
}
