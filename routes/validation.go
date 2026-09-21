package routes

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	dateInputLayout       = "2006-01-02"
	maximumDatabaseAmount = 9999999999.99
	maximumExchangeRate   = 99999999.9999
)

var (
	errInvalidAmount       = errors.New("invalid amount")
	errInvalidDate         = errors.New("invalid date")
	errInvalidExchangeRate = errors.New("invalid exchange rate")
)

func parseDatabaseAmount(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount) > maximumDatabaseAmount {
		return 0, errInvalidAmount
	}
	return amount, nil
}

func parseAmountInputs(amountValue, euroValue string, exchangeRate float64) (float64, error) {
	if math.IsNaN(exchangeRate) || math.IsInf(exchangeRate, 0) || exchangeRate <= 0 || exchangeRate > maximumExchangeRate {
		return 0, errInvalidExchangeRate
	}

	amount, err := parseDatabaseAmount(amountValue)
	if err != nil {
		return 0, err
	}
	if amount < 0 {
		return 0, errInvalidAmount
	}
	if amount != 0 {
		return amount, nil
	}

	euroAmount, err := parseDatabaseAmount(euroValue)
	if err != nil {
		return 0, err
	}
	if euroAmount < 0 {
		return 0, errInvalidAmount
	}
	amount = euroAmount * exchangeRate
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount > maximumDatabaseAmount {
		return 0, errInvalidAmount
	}
	return math.Round(amount*100) / 100, nil
}

func dateWithTime(value string, clock time.Time) (time.Time, error) {
	date, err := time.Parse(dateInputLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, errInvalidDate
	}

	return time.Date(
		date.Year(), date.Month(), date.Day(),
		clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(),
		clock.Location(),
	), nil
}
