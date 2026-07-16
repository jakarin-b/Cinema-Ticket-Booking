package service

import (
	"errors"
	"testing"

	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

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

func TestValidateIdempotencyKey(t *testing.T) {
	if err := validateIdempotencyKey(uuid.NewString()); err != nil {
		t.Fatalf("valid UUID rejected: %v", err)
	}
	for _, test := range []struct {
		value string
		code  string
	}{
		{value: "", code: "IDEMPOTENCY_KEY_REQUIRED"},
		{value: "not-a-uuid", code: "INVALID_IDEMPOTENCY_KEY"},
	} {
		var problem *Error
		if err := validateIdempotencyKey(test.value); !errors.As(err, &problem) || problem.Code != test.code || problem.Status != 400 {
			t.Fatalf("value %q returned %#v, want 400 %s", test.value, err, test.code)
		}
	}
}

func TestSameHoldRequestRequiresSameShowtimeAndNormalizedSeats(t *testing.T) {
	showtimeID := primitive.NewObjectID()
	seatA := primitive.NewObjectID()
	seatB := primitive.NewObjectID()
	hold := domain.Hold{ShowtimeID: showtimeID, SeatIDs: []primitive.ObjectID{seatB, seatA}}
	if !sameHoldRequest(hold, showtimeID, []string{seatA.Hex(), seatB.Hex()}) {
		t.Fatal("same normalized request was not recognized")
	}
	if sameHoldRequest(hold, primitive.NewObjectID(), []string{seatA.Hex(), seatB.Hex()}) {
		t.Fatal("different showtime was treated as the same request")
	}
	if sameHoldRequest(hold, showtimeID, []string{seatA.Hex()}) {
		t.Fatal("different seat selection was treated as the same request")
	}
}
