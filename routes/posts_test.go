package routes

import (
	"testing"
	"time"
)

func TestLinkedPostWriteUsesPrimaryPostDate(t *testing.T) {
	selectedDate := time.Date(2026, time.March, 5, 9, 30, 0, 0, time.UTC)
	primary := postWrite{
		Description: "Rent",
		ExpenseID:   10,
		Amount:      500,
		Exchange:    117.2,
		AccountID:   3,
		PublicID:    "primary",
		CreatedAt:   selectedDate,
	}

	linked := linkedPostWrite(primary, 11, 25, "linked")

	want := postWrite{
		Description: "Rent",
		ExpenseID:   11,
		Amount:      25,
		Exchange:    117.2,
		AccountID:   3,
		PublicID:    "linked",
		CreatedAt:   selectedDate,
	}
	if linked != want {
		t.Fatalf("linkedPostWrite = %+v, want %+v", linked, want)
	}
}
