package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNextDailyRunUsesBelgradeLocalTime(t *testing.T) {
	location, err := time.LoadLocation(backgroundJobsTimezone)
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	tests := []struct {
		name string
		now  time.Time
		hour int
		want time.Time
	}{
		{
			name: "later today",
			now:  time.Date(2026, time.January, 15, 3, 0, 0, 0, location),
			hour: 5,
			want: time.Date(2026, time.January, 15, 5, 0, 0, 0, location),
		},
		{
			name: "tomorrow after scheduled time",
			now:  time.Date(2026, time.January, 15, 8, 0, 0, 0, location),
			hour: 7,
			want: time.Date(2026, time.January, 16, 7, 0, 0, 0, location),
		},
		{
			name: "daylight saving transition",
			now:  time.Date(2026, time.March, 28, 8, 0, 0, 0, location),
			hour: 7,
			want: time.Date(2026, time.March, 29, 7, 0, 0, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextDailyRun(test.now, location, test.hour, 0); !got.Equal(test.want) || got.Location() != location {
				t.Fatalf("nextDailyRun() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBackgroundSchedulerCancelsAndWaitsForRunningJob(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	job := backgroundJob{
		name: "blocking test job",
		hour: 23,
		run: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(finished)
			return ctx.Err()
		},
	}

	scheduler := startBackgroundScheduler(context.Background(), time.UTC, []backgroundJob{job})
	<-started
	scheduler.Stop()

	select {
	case <-finished:
	default:
		t.Fatal("scheduler returned before the running job finished")
	}
	// Stop must be idempotent.
	scheduler.Stop()
}

func TestBackgroundSchedulerRetriesFailedJob(t *testing.T) {
	attempts := 0
	done := make(chan struct{})
	job := backgroundJob{
		name:        "flaky test job",
		hour:        23,
		retryDelays: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		run: func(context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary failure")
			}
			close(done)
			return nil
		},
	}

	scheduler := startBackgroundScheduler(context.Background(), time.UTC, []backgroundJob{job})
	defer scheduler.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("job was not retried until it succeeded")
	}
}

func TestBackgroundSchedulerStopsRetryingAfterLastDelay(t *testing.T) {
	attempts := make(chan struct{}, 10)
	job := backgroundJob{
		name:        "failing test job",
		hour:        23,
		retryDelays: []time.Duration{time.Millisecond, time.Millisecond},
		run: func(context.Context) error {
			attempts <- struct{}{}
			return errors.New("permanent failure")
		},
	}

	scheduler := startBackgroundScheduler(context.Background(), time.UTC, []backgroundJob{job})
	for i := 0; i < 3; i++ {
		select {
		case <-attempts:
		case <-time.After(5 * time.Second):
			t.Fatalf("attempt %d did not run", i+1)
		}
	}
	select {
	case <-attempts:
		t.Fatal("job ran more often than its retry delays allow")
	case <-time.After(50 * time.Millisecond):
	}
	scheduler.Stop()
}

func TestBackgroundSchedulerStopInterruptsRetryWait(t *testing.T) {
	failed := make(chan struct{})
	job := backgroundJob{
		name:        "waiting test job",
		hour:        23,
		retryDelays: []time.Duration{time.Hour},
		run: func(context.Context) error {
			close(failed)
			return errors.New("temporary failure")
		},
	}

	scheduler := startBackgroundScheduler(context.Background(), time.UTC, []backgroundJob{job})
	<-failed

	stopped := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not interrupt the retry wait")
	}
}
