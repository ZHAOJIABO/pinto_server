package ai_generation

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Dispatcher decouples task submission from execution: a create request only
// writes a pending row, and this loop is what actually spends the ~30s a
// provider call takes.
//
// The semaphore is process-local. The provider imposes no concurrency limit of
// its own, so MaxConcurrency is a self-imposed throttle on how much cost and
// memory a burst may consume; raise it from observed queue latency rather than
// guessing. With N instances the effective concurrency becomes N x
// MaxConcurrency, so divide it by N or move to a shared token bucket before
// scaling out.
type Dispatcher struct {
	service     *Service
	sem         chan struct{}
	batchSize   int
	interval    time.Duration
	taskTimeout time.Duration
	shutdown    time.Duration

	notifyCh chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewDispatcher(service *Service, cfg DispatcherConfig) *Dispatcher {
	cfg.applyDefaults()
	return &Dispatcher{
		service:     service,
		sem:         make(chan struct{}, cfg.MaxConcurrency),
		batchSize:   cfg.BatchSize,
		interval:    cfg.Interval,
		taskTimeout: cfg.TaskTimeout,
		shutdown:    cfg.TaskTimeout + 10*time.Second,
		notifyCh:    make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
}

type DispatcherConfig struct {
	MaxConcurrency int
	BatchSize      int
	Interval       time.Duration
	TaskTimeout    time.Duration
}

func (c *DispatcherConfig) applyDefaults() {
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 10
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 200
	}
	if c.Interval <= 0 {
		c.Interval = 2 * time.Second
	}
	if c.TaskTimeout <= 0 {
		c.TaskTimeout = 180 * time.Second
	}
}

func (d *Dispatcher) Start() {
	d.wg.Add(1)
	go d.loop()
	zap.L().Info("ai dispatcher started",
		zap.Int("max_concurrency", cap(d.sem)),
		zap.Duration("interval", d.interval),
		zap.Duration("task_timeout", d.taskTimeout))
}

// Notify wakes the loop immediately after a task is committed, so submission
// latency does not include a full tick.
func (d *Dispatcher) Notify() {
	select {
	case d.notifyCh <- struct{}{}:
	default:
	}
}

// Stop stops claiming new work, then waits for in-flight provider calls. It
// deliberately does not cancel them: the generation is already paid for, and
// anything still running when the grace period ends is recovered by the
// stuck-running reaper.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() { close(d.stopCh) })
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d.shutdown):
		zap.L().Warn("ai dispatcher shutdown grace expired, remaining tasks will be reaped")
	}
	zap.L().Info("ai dispatcher stopped")
}

func (d *Dispatcher) loop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
		case <-d.notifyCh:
		}
		d.drain()
	}
}

// drain claims as many tasks as there are free slots and starts one goroutine
// per task.
//
// Invariant: this loop is the only sender on d.sem. Free slots can therefore
// only grow between measuring and sending, so the sends below never block and a
// task can never be marked running in the database with nobody executing it.
func (d *Dispatcher) drain() {
	for {
		select {
		case <-d.stopCh:
			return
		default:
		}

		free := cap(d.sem) - len(d.sem)
		if free <= 0 {
			return
		}
		if free > d.batchSize {
			free = d.batchSize
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tasks, err := d.service.ClaimBatch(ctx, free)
		cancel()
		if err != nil {
			zap.L().Error("ai dispatcher claim failed", zap.Error(err))
			return
		}
		if len(tasks) == 0 {
			return
		}

		for _, task := range tasks {
			d.sem <- struct{}{}
			d.wg.Add(1)
			go d.run(task.TaskID)
		}
	}
}

func (d *Dispatcher) run(taskID string) {
	defer d.wg.Done()
	defer func() {
		<-d.sem
		// A freed slot should be refilled now, not on the next tick.
		d.Notify()
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			zap.L().Error("ai task panicked",
				zap.String("task_id", taskID),
				zap.Any("panic", recovered),
				zap.Stack("stack"))
			d.service.FailTask(context.Background(), taskID, "internal_panic", "任务执行异常")
		}
	}()

	// A fresh context: the submitting request is long gone, and this call must
	// outlive it.
	ctx, cancel := context.WithTimeout(context.Background(), d.taskTimeout)
	defer cancel()
	d.service.ExecuteTask(ctx, taskID)
}
