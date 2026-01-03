package scheduler

import (
	"context"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
)

// BackgroundScheduler runs the scheduler in a background goroutine.
// Start() returns immediately.
type BackgroundScheduler struct {
	*BaseScheduler
	done chan struct{}
}

// NewBackgroundScheduler creates a new background scheduler.
func NewBackgroundScheduler(cfg Config) *BackgroundScheduler {
	return &BackgroundScheduler{
		BaseScheduler: NewBaseScheduler(cfg),
		done:          make(chan struct{}),
	}
}

// Start begins job processing in a background goroutine.
// Returns immediately.
func (s *BackgroundScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.state != StateStopped {
		s.mu.Unlock()
		return ErrSchedulerAlreadyRunning
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.state = StateRunning
	s.done = make(chan struct{})
	s.mu.Unlock()

	// Start components
	if err := s.startComponents(); err != nil {
		s.mu.Lock()
		s.state = StateStopped
		s.mu.Unlock()
		return err
	}

	s.listeners.DispatchSync(event.NewSchedulerStartedEvent())

	// Run main loop in background
	go s.mainLoop()

	return nil
}

// mainLoop is the main scheduling loop.
func (s *BackgroundScheduler) mainLoop() {
	defer close(s.done)

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
func (s *BackgroundScheduler) Shutdown(wait bool) error {
	s.mu.Lock()
	if s.state == StateStopped {
		s.mu.Unlock()
		return nil
	}

	s.state = StateStopped
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()

	// Wait for main loop to finish
	if wait && done != nil {
		<-done
	}

	s.stopComponents(wait)
	s.listeners.DispatchSync(event.NewSchedulerShutdownEvent())

	return nil
}

// Wait blocks until the scheduler is shut down.
func (s *BackgroundScheduler) Wait() {
	s.mu.RLock()
	done := s.done
	s.mu.RUnlock()

	if done != nil {
		<-done
	}
}
