package lock

import "testing"

func TestSeatKeysAreDeterministic(t *testing.T) {
	keys := Keys("show", []string{"b", "a"})
	if len(keys) != 2 || keys[0] != "seatlock:show:a" || keys[1] != "seatlock:show:b" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if Ownership("hold", "user", "token") != "hold:user:token" {
		t.Fatal("unexpected ownership value")
	}
}
