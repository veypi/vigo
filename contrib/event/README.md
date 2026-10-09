# Event Package

The `event` package provides a robust background task manager for Go applications, supporting both local and distributed task execution.

## Features

- **Flexible Scheduling**: Support for one-time, periodic (`Every`), and scheduled (`At`) tasks.
- **Distributed Locking**: Built-in support for Redis-based distributed locks to ensure tasks run on only one node in a cluster.
- **Graceful Shutdown**: Context-aware task cancellation and wait groups for safe application shutdown.
- **Daemon Mode**: Automatic restart for long-running tasks (`RestartOnFail`).

## Installation

```bash
go get github.com/veypi/vigo/contrib/event
```

## Usage

### 1. Basic Local Task

```go
package main

import (
    "time"
    "github.com/veypi/vigo/contrib/event"
)

func main() {
    // Add a periodic local task (runs every 10 seconds)
    // Key is empty string "" for local-only tasks
    event.Add("", func(ctx context.Context) error {
        println("Local tick")
        return nil
    }, event.Every(10*time.Second))

    // Start the event manager
    event.Start()

    // ... application logic ...

    // Stop all tasks gracefully on shutdown
    defer event.Stop()
}
```

### 2. Distributed Task (Production Ready)

For tasks that should only run once across multiple instances of your application (e.g., daily reports, data sync), use the `Distributed` option with a **unique key**.

```go
package main

import (
    "time"
    "github.com/redis/go-redis/v9"
    "github.com/veypi/vigo/contrib/event"
)

func main() {
    // 1. Configure Redis (Required for distributed tasks)
    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    event.SetRedis(rdb)

    // 2. Add a distributed task
    // - Key: "daily_report" (MUST be unique and consistent across nodes)
    // - Distributed: Sets lock TTL (e.g., 30 minutes)
    event.Add("daily_report", func(ctx context.Context) error {
        println("Generating daily report...")
        // ... heavy lifting ...
        return nil
    }, event.Every(24*time.Hour), event.Distributed(24*time.Hour))

    event.Start()
    defer event.Stop()
}
```

**Distributed lock semantics** (since the 2026-10-08 rewrite):

- **TTL is a crash-recovery bound, not a task-duration estimate.** A watchdog renews the lock every `ttl/3` while the task runs, so long-running tasks are safe with a short TTL. If the holding node crashes, another node takes over after the TTL expires.
- **One-time tasks release the lock on completion** (token-checked, done marker written *before* the release so another node can never see a free lock with no marker). **Periodic tasks keep the lock until the end of the tick**: that is what makes a periodic task run once per period cluster-wide. For periodic tasks the default TTL is the interval — pass an explicit `ttl >= interval` for the strictest once-per-period guarantee (a shorter explicit TTL logs a warning at registration: completed runs are still pinned to the tick boundary, but a crash mid-task lets a peer re-run within the same period once the short TTL lapses). The remaining TTL is re-pinned to the tick boundary after each run, so a task that runs longer than `interval/3` (which triggers a renewal) does not skip the next period. **Daemon tasks (RestartOnFail, no interval) release the lock on return** — success or failure — so the backoff retry can re-acquire it (a stale self-held lock would make the retry look like a peer holds it and the daemon loop would exit as if successful); a crashed holder is still bounded by the TTL. Note there is no standby failover: a node that finds the lock already held skips the run (and a daemon loop treats that skip as success and exits), so only the first holder keeps the daemon alive.
- **Lock loss cancels the task**: if renewal finds the lock stolen (or Redis persistently errors), the task's `ctx` is canceled to prevent dual-active execution. Tasks should respect `ctx`.
- **One-time distributed tasks are deduplicated cluster-wide** via a persistent done marker (`vigo:event:done:{key}`): checked before and after lock acquisition, written on success. A crash before the marker is written means another node reruns the task later — execution is **at-least-once**, so keep task logic idempotent.

### 3. Task Options

| Option | Description | Example |
| :--- | :--- | :--- |
| `event.Every(d)` | Run task periodically every `d` duration. | `event.Every(1 * time.Hour)` |
| `event.At(t)` | Run task once at specific time `t`. | `event.At(time.Now().Add(10*time.Minute))` |
| `event.RestartOnFail()` | Restart task automatically if it returns an error (daemon mode). | `event.RestartOnFail()` |
| `event.Distributed(ttl)` | Use Redis distributed lock with token + renewal + form-specific release (one-time: on completion; periodic: pinned to tick end; daemon: on return). `ttl` is the crash-recovery bound (renewed while running); for periodic tasks use `ttl >= interval` (default: the interval). | `event.Distributed(30 * time.Second)` |
| `event.After(key)` | Run task after `key` task finishes (success, failure or panic — ordering, not success dependency). Invalid for periodic/scheduled tasks. | `event.After("init_task")` |
| `event.Before(key)` | Run task before `key` task starts. Invalid for periodic/scheduled tasks. | `event.Before("final_task")` |

### 4. Task Execution Order

You can define dependencies between one-time tasks using `After` and `Before`.

```go
// Task B runs after Task A completes
event.Add("A", taskA)
event.Add("B", taskB, event.After("A"))

// Equivalent to:
event.Add("A", taskA, event.Before("B"))
event.Add("B", taskB)
```

**Note**:
- Execution order is only supported for **one-time tasks**.
- Periodic (`Every`) and scheduled (`At`) tasks ignore `After` and `Before` options (a warning will be logged).
- Circular dependencies will cause tasks to wait indefinitely.

## API Reference

### `Add(key string, fn TaskFunc, opts ...Option) CancelFunc`

Registers a task.
- **key**: 
  - If `""`: Local task (runs on every node).
  - If `"name"`: Distributed task (runs on one node if `Distributed` option is used).
  - **Warning**: Do not use the same key for different tasks. If a key duplicates, the second task is ignored.
- **fn**: The function to execute (`func(ctx context.Context) error`). The context is canceled on `Stop()`, `Cancel(key)`, or when a distributed task loses its lock mid-run — respect it for graceful shutdown.
- **Returns**: A function to cancel this specific task.

### `Start()` / `Stop()`

Control the lifecycle of the event manager. `Stop()` blocks until all running tasks have completed their current execution cycle.

### `Run(key string) error`

Immediately executes a registered task by its key.
- **key**: The unique identifier of the task.
- **Behavior**:
  - If the task is one-time and has already run, it is skipped.
  - If the task is distributed, it attempts to acquire the lock.
  - Returns an error if the key is not found or empty.

### `List() []string`

Returns a list of all registered task keys.

### `Clear() error`

Removes all distributed locks **and one-time done markers** from Redis for registered tasks.

**Danger**: no ownership check — locks held by tasks currently running on other nodes are deleted too (risking concurrent execution), and removing a done marker lets a one-time task re-run. Use only during a cluster-wide maintenance window.
