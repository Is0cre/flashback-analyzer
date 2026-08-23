package mesh

import (
	"testing"
	"time"
)

func TestCacheObjectIntegrity(t *testing.T) {
	o := NewObject(ThreadPageSnapshot, "flashback", "t123:1", time.Now(), []byte("public page"), LocalObserved)
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.Payload[0] = 'X'
	if err := o.Validate(); err == nil {
		t.Fatal("förväntade hashfel efter ändrad payload")
	}
}

func TestCacheObjectRejectsOversize(t *testing.T) {
	payload := make([]byte, MaxObjectSize+1)
	o := NewObject(ForumSnapshot, "flashback", "root", time.Now(), payload, LocalObserved)
	if err := o.Validate(); err == nil {
		t.Fatal("förväntade avvisning av för stort objekt")
	}
}
