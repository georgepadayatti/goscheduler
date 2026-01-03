package scheduler

import (
	"context"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
)

// BlockingScheduler runs the scheduler in the current goroutine.
// Start() blocks until the scheduler is shut down.
type BlockingScheduler struct {
	*BaseScheduler
}

// NewBlockingScheduler creates a new blocking scheduler.
func NewBlockingScheduler(cfg Config) *BlockingScheduler {
	return &BlockingScheduler{
		BaseScheduler: NewBaseScheduler(cfg),
	}
}

// Start begins job processing and blocks until the context is cancelled
// or Shutdown is called.
func (s *BlockingScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.state != StateStopped {
		s.mu.Unlock()
		return ErrSchedulerAlreadyRunning
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.state = StateRunning
	s.mu.Unlock()

	// Start components
	if err := s.startComponents(); err != nil {
		s.mu.Lock()
		s.state = StateStopped
		s.mu.Unlock()
		return err
	}

	s.listeners.DispatchSync(event.NewSchedulerStartedEvent())

	// Main loop
	s.mainLoop()

	return nil
}

// mainLoop is the main scheduling loop.
func (s *BlockingScheduler) mainLoop() {
	var timer *time.Timer

	for {
		// Process jobs and get wait duration
		wait := s.ProcessJobs()

		// Set up timer
		if timer == nil {
			if wait != nil {
				timer = time.NewTimer(*wait)
			} else {
				// Create a timer that never fires
				timer = time.NewTimer(time.Hour * 24 * 365)
				timer.Stop()
			}
		} else {
			timer.Stop()
			if wait != nil {
				timer.Reset(*wait)
			}
		}

		// Wait for wakeup, timer, or shutdown
		select {
		case <-s.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wakeupCh:
			// Continue to process jobs
		case <-timer.C:
			// Timer expired, process jobs
		}
	}
}

// Shutdown stops the scheduler.
func (s *BlockingScheduler) Shutdown(wait bool) error {
	s.mu.Lock()
	if s.state == StateStopped {
		s.mu.Unlock()
		return nil
	}

	s.state = StateStopped
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	s.stopComponents(wait)
	s.listeners.DispatchSync(event.NewSchedulerShutdownEvent())

	return nil
}
