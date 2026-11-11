package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
	_ "time/tzdata"
)

const backgroundJobsTimezone = "Europe/Belgrade"

type backgroundJob struct {
	name   string
	hour   int
	minute int
	run    func(context.Context) error
	// retryDelays lists the waits before each retry after a failed run.
	retryDelays []time.Duration
}

type backgroundScheduler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	location *time.Location
	jobs     []backgroundJob
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func startBackgroundScheduler(parent context.Context, location *time.Location, jobs []backgroundJob) *backgroundScheduler {
	ctx, cancel := context.WithCancel(parent)
	scheduler := &backgroundScheduler{
		ctx:      ctx,
		cancel:   cancel,
		location: location,
		jobs:     jobs,
	}
	for _, job := range scheduler.jobs {
		scheduler.wg.Add(1)
		go scheduler.run(job)
	}
	return scheduler
}

func (scheduler *backgroundScheduler) run(job backgroundJob) {
	defer scheduler.wg.Done()

	for runImmediately := true; ; runImmediately = false {
		if runImmediately {
			scheduler.execute(job)
		} else {
			now := time.Now()
			nextRun := nextDailyRun(now, scheduler.location, job.hour, job.minute)
			timer := time.NewTimer(nextRun.Sub(now))
			select {
			case <-timer.C:
				scheduler.execute(job)
			case <-scheduler.ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}

		if scheduler.ctx.Err() != nil {
			return
		}
	}
}

func (scheduler *backgroundScheduler) execute(job backgroundJob) {
	for attempt := 0; ; attempt++ {
		err := job.run(scheduler.ctx)
		if err == nil || scheduler.ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		if attempt >= len(job.retryDelays) {
			log.Printf("Background job %q failed: %v", job.name, err)
			return
		}
		delay := job.retryDelays[attempt]
		log.Printf("Background job %q failed, retrying in %v: %v", job.name, delay, err)

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-scheduler.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (scheduler *backgroundScheduler) Stop() {
	scheduler.stopOnce.Do(scheduler.cancel)
	scheduler.wg.Wait()
}

func nextDailyRun(now time.Time, location *time.Location, hour, minute int) time.Time {
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
