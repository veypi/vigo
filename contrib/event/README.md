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
    event.Add("", func() error {
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
    // - Distributed: Sets lock TTL (e.g., 5 minutes)
    event.Add("daily_report", func() error {
        println("Generating daily report...")
        // ... heavy lifting ...
        return nil
    }, event.Every(24*time.Hour), event.Distributed(5*time.Minute))

    event.Start()
    defer event.Stop()
}
```

### 3. Task Options

| Option | Description | Example |
| :--- | :--- | :--- |
| `event.Every(d)` | Run task periodically every `d` duration. | `event.Every(1 * time.Hour)` |
| `event.At(t)` | Run task once at specific time `t`. | `event.At(time.Now().Add(10*time.Minute))` |
| `event.RestartOnFail()` | Restart task automatically if it returns an error (daemon mode). | `event.RestartOnFail()` |
| `event.Distributed(ttl)` | Use Redis distributed lock. `ttl` is lock expiration time. | `event.Distributed(30 * time.Second)` |
| `event.After(key)` | Run task only after `key` task completes successfully. Invalid for periodic/scheduled tasks. | `event.After("init_task")` |
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
- **fn**: The function to execute (`func() error`).
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

Removes all distributed locks from Redis for registered tasks. Useful for cleanup or resetting state.
