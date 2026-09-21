package routes

import (
	"math"
	"testing"
	"time"
)

func TestParseDatabaseAmount(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    float64
		wantErr bool
	}{
		{name: "empty amount", value: "", want: 0},
		{name: "surrounding whitespace", value: " 125.50 ", want: 125.5},
		{name: "negative amount", value: "-42.25", want: -42.25},
		{name: "database maximum", value: "9999999999.99", want: maximumDatabaseAmount},
		{name: "text", value: "twelve", wantErr: true},
		{name: "NaN", value: "NaN", wantErr: true},
		{name: "positive infinity", value: "+Inf", wantErr: true},
		{name: "negative infinity", value: "-Inf", wantErr: true},
		{name: "above database maximum", value: "10000000000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDatabaseAmount(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDatabaseAmount(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseDatabaseAmount(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseAmountInputs(t *testing.T) {
	tests := []struct {
		name         string
		amount       string
		euroAmount   string
		exchangeRate float64
		want         float64
		wantErr      bool
	}{
		{name: "uses dinar amount", amount: "250.25", euroAmount: "invalid but unused", exchangeRate: 117.2, want: 250.25},
		{name: "converts euro amount", euroAmount: "10.25", exchangeRate: 117.2, want: 1201.3},
		{name: "converts amount above 32-bit rounding range", euroAmount: "200000", exchangeRate: 117.2, want: 23440000},
		{name: "allows both amounts to be empty", exchangeRate: 117.2, want: 0},
		{name: "rejects negative dinar amount", amount: "-1", exchangeRate: 117.2, wantErr: true},
		{name: "rejects negative euro amount", euroAmount: "-1", exchangeRate: 117.2, wantErr: true},
		{name: "rejects malformed dinar amount", amount: "invalid", exchangeRate: 117.2, wantErr: true},
		{name: "rejects malformed euro amount", euroAmount: "invalid", exchangeRate: 117.2, wantErr: true},
		{name: "rejects zero exchange rate", amount: "100", exchangeRate: 0, wantErr: true},
		{name: "rejects NaN exchange rate", amount: "100", exchangeRate: math.NaN(), wantErr: true},
		{name: "rejects converted overflow", euroAmount: "9999999999.99", exchangeRate: 117.2, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmountInputs(tt.amount, tt.euroAmount, tt.exchangeRate)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAmountInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseAmountInputs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateWithTime(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	clock := time.Date(2026, time.September, 21, 14, 35, 27, 123, location)

	got, err := dateWithTime("2024-02-29", clock)
	if err != nil {
		t.Fatalf("dateWithTime() error = %v", err)
	}
	want := time.Date(2024, time.February, 29, 14, 35, 27, 123, location)
	if !got.Equal(want) || got.Location() != location {
		t.Errorf("dateWithTime() = %v (%v), want %v (%v)", got, got.Location(), want, want.Location())
	}

	for _, value := range []string{"", "2024-02-30", "21.09.2026", "2026-9-21"} {
		t.Run("rejects "+value, func(t *testing.T) {
			if _, err := dateWithTime(value, clock); err == nil {
				t.Errorf("dateWithTime(%q) accepted an invalid date", value)
			}
		})
	}
}
