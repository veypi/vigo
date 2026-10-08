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
//   - Distributed execution via Redis locks with token ownership and lease
//     renewal; one-time tasks release the lock on completion, periodic tasks
//     hold it for a whole interval (Distributed)
//   - Cluster-wide exactly-once semantics for one-time tasks via done markers
//     (at-least-once execution + done dedup; task logic must be idempotent)
//   - Graceful shutdown (task context cancellation propagates into TaskFunc)
package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo/contrib/config"
	"github.com/veypi/vigo/logv"
)

// TaskFunc defines the function signature for a task.
//
// The context is the task's execution context: it is canceled on manager Stop,
// task Cancel, or — for distributed tasks — when the distributed lock is lost
// mid-execution (renewal failure). Task implementations should respect it:
// a task that ignores ctx cannot be stopped gracefully and will block Stop().
type TaskFunc func(ctx context.Context) error

// CancelFunc cancels a task.
type CancelFunc func()

// Redis key layout:
//   - vigo:event:lock:{key}  分布式锁（token 值 + TTL + 续约）
//   - vigo:event:done:{key}  one-time 任务的集群级完成标记（无 TTL）
const (
	lockKeyPrefix = "vigo:event:lock:"
	doneKeyPrefix = "vigo:event:done:"
)

// 锁续约/释放用 Lua 保证「归属性校验 + 操作」原子：token 不匹配 = 锁已易主，
// 续约失败（返 0）触发任务 ctx 取消（防双活），释放失败（返 0）静默放弃。
var (
	renewLockScript   = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)
	releaseLockScript = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)
)

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
	return &EventManager{
		tasks:              make(map[string]*taskItem),
		pendingReverseDeps: make(map[string][]string),
		doneChans:          make(map[string]chan struct{}),
		orderedKeys:        make([]string, 0),
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
// "After" is an ordering constraint, not a success dependency: dependents run
// after the dependency finishes, whether it succeeded, failed or panicked.
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
// Lock lifecycle (rewritten 2026-10-08):
//   - Acquire: SET lockKey token NX PX ttl — token proves ownership.
//   - Renew: a watchdog renews the lock every ttl/3 while the task runs, so
//     ttl is only a crash-recovery bound (how long the cluster waits before
//     taking over a dead node), NOT an estimate of task duration. Long tasks
//     no longer risk dual execution from TTL expiry.
//   - Lock lost: if renewal finds the lock held by someone else (or Redis
//     errors persist), the task's ctx is canceled to prevent dual-active.
//   - Release: one-time tasks delete the lock immediately on completion
//     (compare-token Lua), so a crash-takeover or a re-run never waits for the
//     TTL. Periodic tasks keep the lock until it expires: the lock then spans a
//     whole tick interval, which is what makes a periodic task run once per
//     period cluster-wide (release-on-completion would let every node's own
//     ticker run it — N executions per period). For periodic tasks the default
//     ttl is therefore the interval; an explicit ttl shorter than the interval
//     gives up the per-period guarantee (registration logs a warning).
//
// One-time distributed tasks additionally use a persistent done marker
// (vigo:event:done:{key}): checked before and after acquiring the lock, set on
// success. Crash before done = another node reruns later (at-least-once; keep
// task logic idempotent).
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

	// Default lock TTL if not set but distributed is enabled.
	// 周期任务：默认 = interval——锁要活到下一个 tick，才能保证「每周期集群一次」
	//（完成即释放会让每个节点的 tick 各跑一遍）。一次性任务：TTL 只是崩溃接管边界
	//（续约跟着任务生命周期走），默认 30s 足够。
	if cfg.distributed && cfg.lockTTL == 0 {
		if cfg.interval > 0 {
			cfg.lockTTL = cfg.interval
		} else {
			cfg.lockTTL = 30 * time.Second
		}
	}
	// 周期任务的周期级去重靠「锁活过一个 interval」：显式 TTL 短于 interval 时
	// 每个节点会各跑一遍（不是双活，但是 N 倍负载）——注册时就提醒，别到线上才发现。
	if cfg.distributed && cfg.interval > 0 && cfg.lockTTL < cfg.interval {
		logv.Warn().Str("id", key).Dur("ttl", cfg.lockTTL).Dur("interval", cfg.interval).
			Msg("event: periodic distributed task has lock ttl < interval; it will run once per node per period (pass ttl >= interval for cluster-wide once-per-period)")
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
				// Channel 满时退到 goroutine 投递，带 ctx 兜底——
				// 此前裸 goroutine 阻塞投递，Stop 后永久泄漏。
				go func() {
					select {
					case e.serialChan <- item:
					case <-e.ctx.Done():
					}
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
				go func() {
					select {
					case e.serialChan <- item:
					case <-e.ctx.Done():
					}
				}()
			}
		} else {
			e.startTask(item)
		}
	}
}

// Stop gracefully shuts down the EventManager.
// It cancels the context for all running tasks and waits for them to finish.
// Tasks that respect their ctx terminate promptly; tasks that ignore ctx can
// still block Stop indefinitely (see TaskFunc documentation).
func (e *EventManager) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	cancel := e.cancel
	e.running = false
	e.mu.Unlock()

	cancel()
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
// If the task is a one-time task and has already been executed, it is skipped
// (locally via executed flag; cluster-wide via the done marker for distributed tasks).
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
	if isOneTime(item) {
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

// Clear removes all distributed locks AND one-time done markers from Redis for
// registered tasks.
//
// 警告：Clear 不做归属性校验——别的节点正在执行的任务其锁也会被删除，
// 可能造成并发双活；done 标记删除会让 one-time 任务重跑。仅在集群静止的
// 维护窗口使用。
func (e *EventManager) Clear() error {
	e.mu.RLock()
	client := e.redisClient
	keys := make([]string, 0, 2*len(e.tasks))
	for k := range e.tasks {
		if k != "" {
			keys = append(keys, lockKeyPrefix+k, doneKeyPrefix+k)
		}
	}
	e.mu.RUnlock()

	if client == nil {
		client = config.SharedRedis()
	}
	if client == nil || len(keys) == 0 {
		return nil
	}

	return client.Del(context.Background(), keys...).Err()
}

// isOneTime reports whether the task is a one-time task (not periodic, not daemon).
func isOneTime(item *taskItem) bool {
	return item.cfg.interval == 0 && !item.cfg.restartOnFail
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

// newLockToken 生成锁归属 token（crypto/rand，避免新增依赖）。
func newLockToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (e *EventManager) executeTask(ctx context.Context, item *taskItem, source string) error {
	// Helper function for running the task.
	// panic 转为非 nil error（带堆栈日志）：此前 recover 后 err 保持 nil，
	// panic 在日志里按成功计（Debug），Run() 调用方也拿不到失败事实。
	doRun := func(ctx context.Context) (err error) {
		start := time.Now()
		defer func() {
			cost := time.Since(start)
			if r := recover(); r != nil {
				err = fmt.Errorf("task %s panic: %v", item.key, r)
				logv.Error().Str("id", item.key).Str("source", source).Dur("duration", cost).
					Str("stack", string(debug.Stack())).Msg(fmt.Sprintf("Task panic recovered: %v", r))
			} else {
				if err != nil {
					logv.WithNoCaller.Error().Err(err).Str("id", item.key).Str("source", source).Dur("duration", cost).Msg("vigo.event")
				} else {
					logv.WithNoCaller.Debug().Str("id", item.key).Str("source", source).Dur("duration", cost).Msg("vigo.event")
				}
			}
		}()
		return item.fn(ctx)
	}

	// Distributed Lock Logic
	if item.cfg.distributed {
		e.mu.RLock()
		client := e.redisClient
		e.mu.RUnlock()
		if client == nil {
			client = config.SharedRedis()
		}

		if client == nil {
			logv.Warn().Msg(fmt.Sprintf("Task %s is marked as distributed but Redis client is not set. Running locally.", item.key))
			return doRun(ctx)
		}

		lockKey := lockKeyPrefix + item.key
		doneKey := doneKeyPrefix + item.key
		oneTime := isOneTime(item)

		// one-time 完成标记：集群级去重（快路径，无锁检查）。
		if oneTime {
			if n, err := client.Exists(ctx, doneKey).Result(); err == nil && n > 0 {
				return nil // 集群内已完成（他节点或本节点前世）
			}
		}

		token := newLockToken()
		success, err := client.SetNX(ctx, lockKey, token, item.cfg.lockTTL).Result()
		if err != nil {
			return fmt.Errorf("redis lock error: %w", err)
		}
		if !success {
			// Lock held by another instance, skip execution
			return nil
		}

		// 拿到锁后复查 done（check→lock 窗口内他节点可能已完成）。
		if oneTime {
			if n, err := client.Exists(ctx, doneKey).Result(); err == nil && n > 0 {
				releaseLockScript.Run(ctx, client, []string{lockKey}, token)
				return nil
			}
		}

		// 任务 ctx：manager 取消之外，锁丢失（续约发现易主/Redis 持续故障）
		// 也取消——双活防护的最后一道（任务应尊重 ctx，见 TaskFunc 文档）。
		taskCtx, cancelTask := context.WithCancel(ctx)
		defer cancelTask()

		// 续约 watchdog：持锁期间每 ttl/3 续期。续约失败（token 不匹配 =
		// 锁已易主，或持续 Redis 错误）→ 取消任务 ctx 并停止续约。
		renewInterval := item.cfg.lockTTL / 3
		if renewInterval <= 0 {
			renewInterval = item.cfg.lockTTL
		}
		watchdogDone := make(chan struct{})
		go func() {
			defer close(watchdogDone)
			ticker := time.NewTicker(renewInterval)
			defer ticker.Stop()
			for {
				select {
				case <-taskCtx.Done():
					return
				case <-ticker.C:
					res, err := renewLockScript.Run(taskCtx, client, []string{lockKey}, token, item.cfg.lockTTL.Milliseconds()).Int()
					if err != nil && taskCtx.Err() != nil {
						return // 任务已结束/被取消
					}
					if err != nil || res == 0 {
						logv.Warn().Str("id", item.key).AnErr("renew_err", err).
							Msg("distributed lock lost (renew failed), cancelling task to prevent dual-active")
						cancelTask()
						return
					}
				}
			}
		}()

		runErr := doRun(taskCtx)

		// 释放策略按任务形态区分（2026-10-08 复核修正）：
		//   - one-time：完成即释放（compare-token）。锁与 done 标记各司其职，
		//     崩溃接管/重跑不必等 TTL。
		//   - 周期任务：**不释放**，锁随 TTL 到期——周期级去重的载体是锁的存活
		//     时间（默认 TTL=interval，见 Add），释放会让每个节点的 tick 各自
		//     拿到锁各跑一遍（N 倍执行）。副作用：任务时长超过 interval 时，
		//     最后一次续约会把锁再延长一个 TTL，最多跳过一个周期——比双活好。
		cancelTask()
		if oneTime {
			releaseLockScript.Run(context.Background(), client, []string{lockKey}, token)
		}
		<-watchdogDone

		// one-time 成功落完成标记（无 TTL）：集群级「只成功一次」的去重依据。
		// 崩溃/失败无标记 → 锁过期后他节点补跑（at-least-once，业务幂等）。
		if oneTime && runErr == nil {
			if err := client.Set(context.Background(), doneKey, time.Now().Format(time.RFC3339Nano), 0).Err(); err != nil {
				logv.Warn().Err(err).Str("id", item.key).Msg("event: write done marker failed; one-time task may re-run on other nodes")
			}
		}
		return runErr
	}

	return doRun(ctx)
}

func (e *EventManager) processSerialTasks() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case item := <-e.serialChan:
			// Stop 时刻 select 伪随机可能再取到一个任务：显式检查，不执行。
			if e.ctx.Err() != nil {
				return
			}
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
			// done = 排序信号而非成功语义（与 FailureDoesNotBlock 一致）：
			// 失败/panic 同样放行后续依赖任务。
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

			e.executeTask(ctx, item, "one-time")
			// 与 serial 路径一致：done = 排序信号，失败/panic 也放行依赖任务。
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

// Clear removes all distributed locks and done markers from Redis for registered
// tasks in the default event manager. See EventManager.Clear for the danger note.
func Clear() error {
	return Default.Clear()
}
