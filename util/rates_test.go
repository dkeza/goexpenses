package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func responseClient(status int, body string, inspect func(*http.Request)) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

func TestFetchExchangeRate(t *testing.T) {
	timestamp := time.Now().UTC().Add(-time.Hour).Unix()
	body := fmt.Sprintf(`{"timestamp":%d,"rates":{"EUR":1,"RSD":117.23456}}`, timestamp)
	client := responseClient(http.StatusOK, body, func(request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", request.Method)
		}
		if request.URL.Query().Get("app_id") != "test-api-key" {
			t.Error("request does not contain the configured API key")
		}
	})

	rate, rateTime, err := fetchExchangeRate(context.Background(), client, "https://rates.example.com/latest.json", "test-api-key", 1024)
	if err != nil {
		t.Fatalf("fetchExchangeRate: %v", err)
	}
	if rate != 117.2346 {
		t.Fatalf("exchange rate = %v, want 117.2346", rate)
	}
	if rateTime.Unix() != timestamp {
		t.Fatalf("exchange rate time = %v, want timestamp %d", rateTime, timestamp)
	}
}

func TestFetchExchangeRateRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		responseLimit int64
		wantError     string
	}{
		{"HTTP error", http.StatusServiceUnavailable, `service unavailable`, 1024, "HTTP 503"},
		{"invalid JSON", http.StatusOK, `{not-json}`, 1024, "invalid JSON"},
		{"oversized response", http.StatusOK, strings.Repeat("x", 65), 64, "too large"},
		{"invalid rates", http.StatusOK, fmt.Sprintf(`{"timestamp":%d,"rates":{"EUR":0,"RSD":117}}`, time.Now().Unix()), 1024, "invalid EUR or RSD"},
		{"stale timestamp", http.StatusOK, `{"timestamp":1,"rates":{"EUR":1,"RSD":117}}`, 1024, "stale or future timestamp"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := responseClient(test.status, test.body, nil)
			_, _, err := fetchExchangeRate(context.Background(), client, "https://rates.example.com/latest.json", "secret-key", test.responseLimit)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("fetchExchangeRate error = %v, want text %q", err, test.wantError)
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("fetchExchangeRate error exposed API key: %v", err)
			}
		})
	}
}

func TestFetchExchangeRateTimesOutWithoutExposingAPIKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _, err := fetchExchangeRate(ctx, client, "https://rates.example.com/latest.json", "timeout-secret-key", 1024)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("fetchExchangeRate error = %v, want timeout", err)
	}
	if strings.Contains(err.Error(), "timeout-secret-key") {
		t.Fatalf("fetchExchangeRate error exposed API key: %v", err)
	}
}

func TestFetchExchangeRateHidesAPIKeyOnTransportError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	})}
	_, _, err := fetchExchangeRate(context.Background(), client, "https://rates.example.com/latest.json", "transport-secret-key", 1024)
	if err == nil || strings.Contains(err.Error(), "transport-secret-key") {
		t.Fatalf("unsafe exchange rate transport error: %v", err)
	}
}

func TestUpdateExchangeRatesUpsertsValidatedRate(t *testing.T) {
	timestamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	body := fmt.Sprintf(`{"timestamp":%d,"rates":{"EUR":2,"RSD":234.4}}`, timestamp.Unix())
	client := responseClient(http.StatusOK, body, nil)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	query := `
		INSERT INTO currencies (code, rate, date)
		VALUES ($1, $2, $3)
		ON CONFLICT (code) DO UPDATE
		SET rate = EXCLUDED.rate,
		    date = EXCLUDED.date`
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("EUR", 117.2, timestamp).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rate, date, err := updateExchangeRates(context.Background(), client, "https://rates.example.com/latest.json", "api-key", sqlxDB)
	if err != nil {
		t.Fatalf("updateExchangeRates: %v", err)
	}
	if rate != 117.2 || date != timestamp.Format("2006-01-02 15:04:05") {
		t.Fatalf("updated exchange rate = %v, %q", rate, date)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestUpdateExchangeRatesDoesNotWriteInvalidResponse(t *testing.T) {
	client := responseClient(http.StatusOK, `{invalid}`, nil)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	_, _, err = updateExchangeRates(context.Background(), client, "https://rates.example.com/latest.json", "api-key", sqlx.NewDb(db, "sqlmock"))
	if err == nil {
		t.Fatal("updateExchangeRates accepted an invalid response")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("invalid response changed the database: %v", err)
	}
}
