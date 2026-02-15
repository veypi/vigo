// Package event provides a robust background task manager with support for
// one-time, periodic, scheduled, and distributed tasks.
//
// It offers a simple API to register tasks that can run locally or across
// multiple nodes using Redis for distributed locking.
//
// Key features:
//   - Periodic tasks (Every)
//   - Scheduled tasks (At)
//   - Daemon/Restart-on-fail tasks (RestartOnFail)
//   - Distributed locking via Redis (Distributed)
//   - Graceful shutdown
package event

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo/contrib/config"
	"github.com/veypi/vigo/logv"
)

// TaskFunc defines the function signature for a task.
type TaskFunc func() error

// CancelFunc cancels a task.
type CancelFunc func()

// EventManager manages the lifecycle of background tasks.
// It supports local and distributed task execution, graceful shutdown,
// and dynamic task management.
type EventManager struct {
	tasks              map[string]*taskItem
	pendingReverseDeps map[string][]string // key -> list of tasks that must run before key
	orderedKeys        []string            // Maintain addition order for serial execution
	mu                 sync.RWMutex
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	running            bool
	redisClient        *redis.Client
	doneChans          map[string]chan struct{}
	serialChan         chan *taskItem // Channel for serial execution of simple one-time tasks
}

// Default is the global task manager instance used by package-level functions.
var Default = NewEventManager()

// NewEventManager creates a new, independent EventManager.
// Most users should use the package-level Add/Start/Stop functions instead.
func NewEventManager() *EventManager {
	client := config.Redis{Addr: "memory"}
	return &EventManager{
		tasks:              make(map[string]*taskItem),
		pendingReverseDeps: make(map[string][]string),
		doneChans:          make(map[string]chan struct{}),
		orderedKeys:        make([]string, 0),
		redisClient:        client.Client(),
		serialChan:         make(chan *taskItem, 1024), // Buffered channel for serial tasks
	}
}

type taskItem struct {
	key      string
	fn       TaskFunc
	cfg      *taskConfig
	cancel   context.CancelFunc
	executed atomic.Bool
}

type taskConfig struct {
	interval      time.Duration
	startAt       time.Time
	restartOnFail bool
	distributed   bool
	lockTTL       time.Duration
	after         []string // List of tasks that must complete before this task
	before        []string // List of tasks that must wait for this task
}

// Option configures a task.
type Option func(*taskConfig)

// After sets the task to run after the specified task key.
// Invalid for periodic (Every) or scheduled (At) tasks.
func After(key string) Option {
	return func(c *taskConfig) {
		c.after = append(c.after, key)
	}
}

// Before sets the task to run before the specified task key.
// Invalid for periodic (Every) or scheduled (At) tasks.
func Before(key string) Option {
	return func(c *taskConfig) {
		c.before = append(c.before, key)
	}
}

// Every sets the task to run periodically.
func Every(d time.Duration) Option {
	return func(c *taskConfig) {
		c.interval = d
	}
}

// At sets the task to run at a specific time.
func At(t time.Time) Option {
	return func(c *taskConfig) {
		c.startAt = t
	}
}

// RestartOnFail ensures the task restarts if it returns an error.
// This is suitable for long-running tasks or daemons.
func RestartOnFail() Option {
	return func(c *taskConfig) {
		c.restartOnFail = true
	}
}

// Distributed marks the task as a distributed task that requires a Redis lock.
//
// This ensures that even if the task is registered on multiple nodes,
// only one node will execute it at a time.
//
// Parameters:
//   - ttl: The duration of the distributed lock.
//     It should be slightly longer than the expected task execution time
//     but shorter than the task interval (for periodic tasks).
//
// If Redis client is not set (via SetRedis), this option is ignored and the task runs locally.
func Distributed(ttl time.Duration) Option {
	return func(c *taskConfig) {
		c.distributed = true
		c.lockTTL = ttl
	}
}

// SetRedis configures the Redis client used for distributed locking.
// This must be called before Start() if you intend to use Distributed() tasks.
func (e *EventManager) SetRedis(client *redis.Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.redisClient = client
}

// Add registers a new task with the EventManager.
//
// Parameters:
//   - key: A unique identifier for the task.
//     If empty, the task is considered local-only (even if Distributed option is set).
//     If a task with the same key already exists, the new registration is ignored (no-op).
//   - fn: The function to execute.
//   - opts: Configuration options (e.g., Every, At, Distributed).
//
// Returns:
//   - A CancelFunc that can be called to stop and remove the task.
func (e *EventManager) Add(key string, fn TaskFunc, opts ...Option) CancelFunc {
	cfg := &taskConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Distributed tasks MUST have a key
	if cfg.distributed && key == "" {
		logv.Warn().Msg("Distributed task defined without a key. Disabling distributed mode.")
		cfg.distributed = false
	}

	// Validate Before/After for periodic/scheduled tasks
	if (cfg.interval > 0 || !cfg.startAt.IsZero()) && (len(cfg.after) > 0 || len(cfg.before) > 0) {
		logv.Warn().Msg(fmt.Sprintf("Task '%s' has execution order (Before/After) but is periodic or scheduled. Ignoring order constraints.", key))
		cfg.after = nil
		cfg.before = nil
	}

	// Default lock TTL if not set but distributed is enabled
	if cfg.distributed && cfg.lockTTL == 0 {
		if cfg.interval > 0 {
			cfg.lockTTL = cfg.interval
		} else {
			cfg.lockTTL = 1 * time.Minute
		}
	}

	item := &taskItem{
		key: key,
		fn:  fn,
		cfg: cfg,
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicates if key is provided
	if key != "" {
		if _, exists := e.tasks[key]; exists {
			logv.Warn().Msg(fmt.Sprintf("Task with key '%s' already exists. Ignoring new task registration.", key))
			// Return a no-op cancel function since we didn't add the task
			return func() {}
		}
		e.tasks[key] = item
		e.orderedKeys = append(e.orderedKeys, key)

		// Handle Before: this task must run before others
		// i.e., others depend on this task
		for _, targetKey := range cfg.before {
			if target, ok := e.tasks[targetKey]; ok {
				// If the manager is running, existing tasks have already been started.
				// We cannot safely inject a dependency into a running task.
				if e.running {
					logv.Warn().Msg(fmt.Sprintf("Task '%s' is already running. Cannot add 'Before' dependency from '%s'.", targetKey, key))
					continue
				}
				target.cfg.after = append(target.cfg.after, key)
			} else {
				// Target task not yet registered, store in pendingReverseDeps
				// key runs before targetKey => targetKey runs after key
				e.pendingReverseDeps[targetKey] = append(e.pendingReverseDeps[targetKey], key)
			}
		}

		// Check if any previously registered tasks declared they run before this task
		// i.e., this task depends on them
		if deps, ok := e.pendingReverseDeps[key]; ok {
			cfg.after = append(cfg.after, deps...)
			delete(e.pendingReverseDeps, key)
		}
	} else {
		// If no key, we can't store it in the map by name.
		// We need a unique internal ID to track it for cancellation.
		// For now, let's generate a random internal ID just for storage.
		internalKey := fmt.Sprintf("anon_%d", time.Now().UnixNano())
		item.key = internalKey // Update item key
		e.tasks[internalKey] = item
		e.orderedKeys = append(e.orderedKeys, internalKey)
	}

	if e.running {
		if item.cfg.interval == 0 && !item.cfg.restartOnFail && item.cfg.startAt.IsZero() && len(item.cfg.after) == 0 {
			// Simple one-time task: send to serial channel
			select {
			case e.serialChan <- item:
			default:
				// If channel is full, launch a goroutine to avoid blocking
				go func() {
					e.serialChan <- item
				}()
			}
		} else {
			e.startTask(item)
		}
	}

	// Return a cancel function for this specific task
	// Capture the actual key used for storage
	storedKey := item.key
	return func() {
		e.Cancel(storedKey)
	}
}

// Start launches the EventManager in the background.
// It initializes the context and starts all registered tasks.
// If the manager is already running, this is a no-op.
func (e *EventManager) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return
	}

	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.running = true

	// Launch serial worker
	e.wg.Add(1)
	go e.processSerialTasks()

	for _, key := range e.orderedKeys {
		item, ok := e.tasks[key]
		if !ok {
			continue
		}

		if item.cfg.interval == 0 && !item.cfg.restartOnFail && item.cfg.startAt.IsZero() && len(item.cfg.after) == 0 {
			// Simple one-time task
			select {
			case e.serialChan <- item:
			default:
				go func() { e.serialChan <- item }()
			}
		} else {
			e.startTask(item)
		}
	}
}

// Stop gracefully shuts down the EventManager.
// It cancels the context for all running tasks and waits for them to finish.
// This function blocks until all tasks have completed or the context is cancelled.
func (e *EventManager) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	e.cancel()
	e.running = false
	e.wg.Wait()
}

// Cancel stops a specific task by its key and removes it from the registry.
// If the task is running, its context will be cancelled.
// If the task is not found, this is a no-op.
func (e *EventManager) Cancel(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if item, ok := e.tasks[key]; ok {
		if item.cancel != nil {
			item.cancel()
		}
		delete(e.tasks, key)
	}
}

// Run immediately executes a registered task.
// If the key is empty, it returns an error.
// If the task is a one-time task and has already been executed, it is skipped.
// If the task is distributed, it attempts to acquire the lock before execution.
func (e *EventManager) Run(key string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	e.mu.RLock()
	item, ok := e.tasks[key]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task with key '%s' not found", key)
	}

	// Check if one-time task has already been executed
	isOneTime := item.cfg.interval == 0 && !item.cfg.restartOnFail
	if isOneTime {
		if !item.executed.CompareAndSwap(false, true) {
			return nil // Already executed, skip
		}
	}

	// Execute task (respecting distributed lock if configured)
	err := e.executeTask(context.Background(), item, "manual")
	if err == nil {
		e.markDone(key)
	}
	return err
}

// List returns a list of all registered task keys.
func (e *EventManager) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	keys := make([]string, 0, len(e.tasks))
	for k := range e.tasks {
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// Clear removes all distributed locks from Redis for registered tasks.
func (e *EventManager) Clear() error {
	e.mu.RLock()
	client := e.redisClient
	keys := make([]string, 0, len(e.tasks))
	for k := range e.tasks {
		if k != "" {
			keys = append(keys, fmt.Sprintf("vigo:event:lock:%s", k))
		}
	}
	e.mu.RUnlock()

	if client == nil || len(keys) == 0 {
		return nil
	}

	return client.Del(context.Background(), keys...).Err()
}

// getDoneChan returns the completion channel for a task.
// If the channel doesn't exist, it creates one.
func (e *EventManager) getDoneChan(key string) chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ch, ok := e.doneChans[key]; ok {
		return ch
	}

	ch := make(chan struct{})
	e.doneChans[key] = ch
	return ch
}

// markDone closes the completion channel for a task if it hasn't been closed yet.
func (e *EventManager) markDone(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ch, ok := e.doneChans[key]
	if !ok {
		ch = make(chan struct{})
		e.doneChans[key] = ch
	}

	select {
	case <-ch:
		// Already closed
	default:
		close(ch)
	}
}

func (e *EventManager) executeTask(ctx context.Context, item *taskItem, source string) error {
	// Helper function for running the task
	doRun := func() (err error) {
		start := time.Now()
		defer func() {
			cost := time.Since(start)
			if r := recover(); r != nil {
				logv.Error().Str("id", item.key).Str("source", source).Dur("duration", cost).Msg(fmt.Sprintf("Task panic recovered: %v", r))
			} else {
				if err != nil {
					logv.WithNoCaller.Error().Err(err).Str("id", item.key).Str("source", source).Dur("duration", cost).Msg("vigo.event")
				} else {
					logv.WithNoCaller.Info().Str("id", item.key).Str("source", source).Dur("duration", cost).Msg("vigo.event")
				}
			}
		}()
		return item.fn()
	}

	// Distributed Lock Logic
	if item.cfg.distributed {
		e.mu.RLock()
		client := e.redisClient
		e.mu.RUnlock()

		if client == nil {
			logv.Warn().Msg(fmt.Sprintf("Task %s is marked as distributed but Redis client is not set. Running locally.", item.key))
			return doRun()
		}

		lockKey := fmt.Sprintf("vigo:event:lock:%s", item.key)
		// Try to acquire lock
		// Use SetNX to ensure only one instance runs within the TTL period
		success, err := client.SetNX(ctx, lockKey, "locked", item.cfg.lockTTL).Result()
		if err != nil {
			return fmt.Errorf("redis lock error: %w", err)
		}
		if !success {
			// Lock held by another instance, skip execution
			// logv.Debug().Msg(fmt.Sprintf("Task %s skipped (lock held by another node)", item.key))
			return nil
		}
		// Lock acquired, run the task
		return doRun()
	}

	return doRun()
}

func (e *EventManager) processSerialTasks() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case item := <-e.serialChan:
			// Check if task was cancelled (removed from map) before execution
			e.mu.RLock()
			_, exists := e.tasks[item.key]
			e.mu.RUnlock()
			if !exists {
				continue
			}

			// Check if already executed (e.g. by manual Run)
			if !item.executed.CompareAndSwap(false, true) {
				continue
			}

			// Create context for this task
			ctx, cancel := context.WithCancel(e.ctx)
			item.cancel = cancel

			// Execute
			e.executeTask(ctx, item, "serial")
			e.markDone(item.key)
			cancel()
		}
	}
}

func (e *EventManager) startTask(item *taskItem) {
	ctx, cancel := context.WithCancel(e.ctx)
	item.cancel = cancel

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer cancel()

		// Handle start delay
		if !item.cfg.startAt.IsZero() {
			delay := time.Until(item.cfg.startAt)
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}

		// Periodic execution
		if item.cfg.interval > 0 {
			ticker := time.NewTicker(item.cfg.interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					e.executeTask(ctx, item, "periodic")
				}
			}
		} else if item.cfg.restartOnFail {
			// Daemon / Long-running task that should be restarted on failure
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if err := e.executeTask(ctx, item, "daemon"); err != nil {
						logv.Warn().Msg("Task failed, restarting in 1s")
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Second):
							// Backoff
						}
					} else {
						// Task completed successfully
						return
					}
				}
			}
		} else {
			// Run once
			// Check if already executed (e.g. by manual Run)
			if !item.executed.CompareAndSwap(false, true) {
				return
			}

			// Handle execution order (After dependencies)
			for _, depKey := range item.cfg.after {
				ch := e.getDoneChan(depKey)
				select {
				case <-ctx.Done():
					return
				case <-ch:
					// Dependency completed
				}
			}

			// Double check execution status after waiting
			// (though compare-and-swap above should handle it, but for clarity)
			// Actually, if we waited, we still hold the "reservation" via the CAS above.

			e.executeTask(ctx, item, "one-time")
			// Signal completion
			e.markDone(item.key)
		}
	}()
}

// --- Package-level wrappers for Default EventManager ---

// SetRedis sets the Redis client for the default event manager.
func SetRedis(client *redis.Client) {
	Default.SetRedis(client)
}

// Add adds a task to the default event manager.
func Add(key string, fn TaskFunc, opts ...Option) CancelFunc {
	return Default.Add(key, fn, opts...)
}

// Start starts the default event manager.
func Start() {
	Default.Start()
}

// Stop stops the default event manager.
func Stop() {
	Default.Stop()
}

// Cancel cancels a task in the default event manager.
func Cancel(key string) {
	Default.Cancel(key)
}

// Run immediately executes a registered task in the default event manager.
func Run(key string) error {
	return Default.Run(key)
}

// List returns a list of all registered task keys in the default event manager.
func List() []string {
	return Default.List()
}

// Clear removes all distributed locks from Redis for registered tasks in the default event manager.
func Clear() error {
	return Default.Clear()
}
