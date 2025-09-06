// Package async includes helpers for scheduling runnable, periodic functions and contains useful helpers for converting multi-processor computation.
package async

import (
	"context"
	"reflect"
	"runtime"
	"time"

	"github.com/OffchainLabs/prysm/v6/time/slots"
	log "github.com/sirupsen/logrus"
)

// RunEvery runs the provided command periodically.
// It runs in a goroutine, and can be cancelled by finishing the supplied context.
func RunEvery(ctx context.Context, interval time.Duration, f func()) {
	period := func() time.Duration {
		return interval
	}
	runEvery(ctx, period, f)
}

// RunEverySlotDivision runs the provided command periodically.
// It runs in a goroutine, and can be cancelled by finishing the supplied context.
func RunEverySlotDivision(ctx context.Context, genesisTime time.Time, division uint64, f func()) {
	period := func() time.Duration {
		return time.Duration(slots.CurrentMillisecondsPerSlot(genesisTime)/division) * time.Millisecond
	}
	runEvery(ctx, period, f)
}

// RunEvery runs the provided command periodically.
// It runs in a goroutine, and can be cancelled by finishing the supplied context.
func RunEverySlotMultiple(ctx context.Context, genesisTime time.Time, multiple uint64, f func()) {
	period := func() time.Duration {
		slot := slots.CurrentSlot(genesisTime)
		return slots.SecondsInSlotRange(slot, slot.Add(multiple))
	}
	runEvery(ctx, period, f)
}

// runEvery runs the provided command periodically.
func runEvery(ctx context.Context, period func() time.Duration, f func()) {
	funcName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	go func() {
		for {
			select {
			case <-time.After(period()):
				log.WithField("function", funcName).Trace("Running")
				f()
			case <-ctx.Done():
				log.WithField("function", funcName).Debug("Context is closed, exiting")
				return
			}
		}
	}()
}
