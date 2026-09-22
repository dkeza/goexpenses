package main

import (
	"context"
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
