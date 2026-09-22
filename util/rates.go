package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"goexpenses/database"

	"github.com/jmoiron/sqlx"
)

const (
	exchangeRatesEndpoint    = "https://openexchangerates.org/api/latest.json"
	exchangeRateRequestLimit = int64(1 << 20)
	exchangeRateTimeout      = 10 * time.Second
	exchangeRateMaxAge       = 7 * 24 * time.Hour
	exchangeRateRetryDelay   = time.Minute
)

var (
	exchangeRateHTTPClient = &http.Client{
		Timeout: exchangeRateTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	exchangeRateRefreshing  atomic.Bool
	exchangeRateLastAttempt atomic.Int64
	exchangeRateAsyncState  struct {
		mu        sync.Mutex
		ctx       context.Context
		cancel    context.CancelFunc
		accepting bool
		running   bool
		wg        sync.WaitGroup
	}
)

type exchangeRatesResponse struct {
	Timestamp int64              `json:"timestamp"`
	Rates     map[string]float64 `json:"rates"`
}

func RefreshExchangeRates(ctx context.Context) error {
	if !exchangeRateRefreshing.CompareAndSwap(false, true) {
		return nil
	}
	defer exchangeRateRefreshing.Store(false)

	now := time.Now()
	lastAttempt := exchangeRateLastAttempt.Load()
	if lastAttempt != 0 && now.Sub(time.Unix(0, lastAttempt)) < exchangeRateRetryDelay {
		return nil
	}
	exchangeRateLastAttempt.Store(now.UnixNano())

	requestContext, cancel := context.WithTimeout(ctx, exchangeRateTimeout)
	defer cancel()

	rate, _, err := updateExchangeRates(requestContext, exchangeRateHTTPClient, exchangeRatesEndpoint, Settings.OpenExchangeRatesId, database.Db)
	if err != nil {
		return err
	}
	log.Printf("Updated EUR exchange rate: %.4f", rate)
	return nil
}

func StartExchangeRateRefreshWorker(parent context.Context) (func(), error) {
	exchangeRateAsyncState.mu.Lock()
	defer exchangeRateAsyncState.mu.Unlock()
	if exchangeRateAsyncState.running {
		return nil, errors.New("exchange rate refresh worker is already running")
	}

	ctx, cancel := context.WithCancel(parent)
	exchangeRateAsyncState.ctx = ctx
	exchangeRateAsyncState.cancel = cancel
	exchangeRateAsyncState.accepting = true
	exchangeRateAsyncState.running = true

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			exchangeRateAsyncState.mu.Lock()
			exchangeRateAsyncState.accepting = false
			exchangeRateAsyncState.cancel()
			exchangeRateAsyncState.mu.Unlock()

			exchangeRateAsyncState.wg.Wait()

			exchangeRateAsyncState.mu.Lock()
			exchangeRateAsyncState.ctx = nil
			exchangeRateAsyncState.cancel = nil
			exchangeRateAsyncState.running = false
			exchangeRateAsyncState.mu.Unlock()
		})
	}, nil
}

func RefreshExchangeRatesAsync() {
	exchangeRateAsyncState.mu.Lock()
	if !exchangeRateAsyncState.accepting {
		exchangeRateAsyncState.mu.Unlock()
		return
	}
	ctx := exchangeRateAsyncState.ctx
	exchangeRateAsyncState.wg.Add(1)
	exchangeRateAsyncState.mu.Unlock()

	go func() {
		defer exchangeRateAsyncState.wg.Done()
		if err := RefreshExchangeRates(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Could not update exchange rates: %v", err)
		}
	}()
}

func updateExchangeRates(ctx context.Context, client *http.Client, endpoint, apiKey string, db *sqlx.DB) (float64, string, error) {
	rate, rateTime, err := fetchExchangeRate(ctx, client, endpoint, apiKey, exchangeRateRequestLimit)
	if err != nil {
		return 0, "", err
	}
	if err := storeExchangeRate(ctx, db, rate, rateTime); err != nil {
		return 0, "", err
	}
	return rate, rateTime.Format("2006-01-02 15:04:05"), nil
}

func fetchExchangeRate(ctx context.Context, client *http.Client, endpoint, apiKey string, responseLimit int64) (float64, time.Time, error) {
	if client == nil {
		return 0, time.Time{}, errors.New("exchange rate HTTP client is missing")
	}
	if responseLimit < 1 {
		return 0, time.Time{}, errors.New("exchange rate response limit is invalid")
	}

	requestURL, err := url.Parse(endpoint)
	if err != nil || (requestURL.Scheme != "http" && requestURL.Scheme != "https") || requestURL.Host == "" {
		return 0, time.Time{}, errors.New("exchange rate endpoint is invalid")
	}
	query := requestURL.Query()
	query.Set("app_id", apiKey)
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return 0, time.Time{}, errors.New("create exchange rate request")
	}
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return 0, time.Time{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, time.Time{}, errors.New("exchange rate request timed out")
		}
		return 0, time.Time{}, errors.New("exchange rate request failed")
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, time.Time{}, fmt.Errorf("exchange rate service returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return 0, time.Time{}, errors.New("read exchange rate response")
	}
	if int64(len(body)) > responseLimit {
		return 0, time.Time{}, errors.New("exchange rate response is too large")
	}

	payload := exchangeRatesResponse{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, time.Time{}, errors.New("exchange rate response contains invalid JSON")
	}
	eur, eurOK := payload.Rates["EUR"]
	rsd, rsdOK := payload.Rates["RSD"]
	if !eurOK || !rsdOK || eur <= 0 || rsd <= 0 || math.IsNaN(eur) || math.IsNaN(rsd) || math.IsInf(eur, 0) || math.IsInf(rsd, 0) {
		return 0, time.Time{}, errors.New("exchange rate response contains invalid EUR or RSD rates")
	}
	if payload.Timestamp <= 0 {
		return 0, time.Time{}, errors.New("exchange rate response contains an invalid timestamp")
	}
	rateTime := time.Unix(payload.Timestamp, 0).UTC()
	now := time.Now().UTC()
	if rateTime.Before(now.Add(-exchangeRateMaxAge)) || rateTime.After(now.Add(time.Hour)) {
		return 0, time.Time{}, errors.New("exchange rate response contains a stale or future timestamp")
	}

	rate := ToFixed(rsd/eur, 4)
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, time.Time{}, errors.New("calculated EUR exchange rate is invalid")
	}
	return rate, rateTime, nil
}

func storeExchangeRate(ctx context.Context, db *sqlx.DB, rate float64, rateTime time.Time) error {
	if db == nil {
		return errors.New("store exchange rate: database is not connected")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO currencies (code, rate, date)
		VALUES ($1, $2, $3)
		ON CONFLICT (code) DO UPDATE
		SET rate = EXCLUDED.rate,
		    date = EXCLUDED.date`, "EUR", rate, rateTime)
	if err != nil {
		return fmt.Errorf("store exchange rate: %w", err)
	}
	return nil
}

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(Round(num*output)) / output
}

func Round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}
