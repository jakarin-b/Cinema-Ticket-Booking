package service

import "testing"

func TestNormalizeIDsDeduplicatesAndSorts(t *testing.T) {
	a := "650000000000000000000002"
	b := "650000000000000000000001"
	ids, values, err := normalizeIDs([]string{a, b, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || len(values) != 2 || values[0] != b || values[1] != a {
		t.Fatalf("unexpected normalized IDs: %#v", values)
	}
}

func TestNormalizeIDsRejectsMalformedValue(t *testing.T) {
	if _, _, err := normalizeIDs([]string{"not-an-object-id"}); err == nil {
		t.Fatal("expected malformed ObjectID to fail")
	}
}
