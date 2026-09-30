package util

import (
	"context"
	"regexp"
	"testing"

	"goexpenses/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

const eurRateQuery = "SELECT id, code, rate, date FROM currencies WHERE code = $1"

func useRateCacheMock(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	previousDB := database.Db
	database.Db = sqlx.NewDb(db, "sqlmock")
	InvalidateEURRateCache()
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("database expectations: %v", err)
		}
		database.Db = previousDB
		InvalidateEURRateCache()
		db.Close()
	})
	return mock
}

func expectEURRate(mock sqlmock.Sqlmock, rate float64) {
	rows := sqlmock.NewRows([]string{"id", "code", "rate", "date"})
	if rate > 0 {
		rows.AddRow(1, "EUR", rate, "2026-09-30 07:00:00")
	}
	mock.ExpectQuery(regexp.QuoteMeta(eurRateQuery)).WithArgs("EUR").WillReturnRows(rows)
}

func TestCurrentEURRateIsCached(t *testing.T) {
	mock := useRateCacheMock(t)
	expectEURRate(mock, 117.2)

	for range 3 {
		currency, err := CurrentEURRate(context.Background())
		if err != nil {
			t.Fatalf("CurrentEURRate: %v", err)
		}
		if currency.Rate != 117.2 {
			t.Fatalf("rate = %v, want 117.2", currency.Rate)
		}
	}
}

func TestCurrentEURRateReloadsAfterInvalidation(t *testing.T) {
	mock := useRateCacheMock(t)
	expectEURRate(mock, 117.2)
	expectEURRate(mock, 117.3)

	if _, err := CurrentEURRate(context.Background()); err != nil {
		t.Fatalf("CurrentEURRate: %v", err)
	}
	InvalidateEURRateCache()
	currency, err := CurrentEURRate(context.Background())
	if err != nil {
		t.Fatalf("CurrentEURRate: %v", err)
	}
	if currency.Rate != 117.3 {
		t.Fatalf("rate after invalidation = %v, want 117.3", currency.Rate)
	}
}

func TestCurrentEURRateDoesNotCacheMissingRate(t *testing.T) {
	mock := useRateCacheMock(t)
	expectEURRate(mock, 0)
	expectEURRate(mock, 117.2)

	currency, err := CurrentEURRate(context.Background())
	if err != nil || currency.Rate != 0 {
		t.Fatalf("missing rate = %v, %v", currency.Rate, err)
	}
	currency, err = CurrentEURRate(context.Background())
	if err != nil || currency.Rate != 117.2 {
		t.Fatalf("rate after it was stored = %v, %v", currency.Rate, err)
	}
}
