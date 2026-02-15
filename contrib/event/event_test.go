package event

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo/logv"
)

func TestEvent(t *testing.T) {
	logv.DisableCaller()

	// Setup MiniRedis
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	t.Run("LocalTask", func(t *testing.T) {
		e := NewEventManager()
		var counter int32
		e.Add("local_task", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}, Every(50*time.Millisecond))

		e.Start()
		time.Sleep(200 * time.Millisecond)
		e.Stop()

		val := atomic.LoadInt32(&counter)
		if val < 3 {
			t.Errorf("expected counter >= 3, got %d", val)
		}
	})

	t.Run("DistributedTask_SingleNode", func(t *testing.T) {
		e := NewEventManager()
		e.SetRedis(rdb)

		var counter int32
		// Interval 100ms. Test 450ms.
		// Ticks at 100, 200, 300, 400.
		// Lock TTL 50ms.
		e.Add("dist_task_1", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}, Every(100*time.Millisecond), Distributed(50*time.Millisecond))

		e.Start()

		// Advance miniredis time concurrently with sleep
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					s.FastForward(20 * time.Millisecond)
				}
			}
		}()

		time.Sleep(450 * time.Millisecond)
		close(done)
		e.Stop()

		val := atomic.LoadInt32(&counter)
		if val < 3 {
			t.Errorf("expected counter >= 3, got %d", val)
		}
	})

	t.Run("DistributedTask_MultiNode_LockContention", func(t *testing.T) {
		// Simulate 2 nodes
		e1 := NewEventManager()
		e1.SetRedis(rdb)
		e2 := NewEventManager()
		e2.SetRedis(rdb)

		var counter int32
		taskFn := func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}

		// Interval 100ms. Test 450ms.
		// Lock TTL 50ms.
		opts := []Option{Every(100 * time.Millisecond), Distributed(50 * time.Millisecond)}

		e1.Add("shared_task", taskFn, opts...)
		e2.Add("shared_task", taskFn, opts...)

		e1.Start()
		e2.Start()

		// Advance miniredis time concurrently with sleep
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					s.FastForward(20 * time.Millisecond)
				}
			}
		}()

		time.Sleep(450 * time.Millisecond)
		close(done)

		e1.Stop()
		e2.Stop()

		val := atomic.LoadInt32(&counter)
		// Should be around 4 executions (1 per 100ms interval, shared by 2 nodes)
		if val > 6 {
			t.Errorf("lock failed, too many executions: %d", val)
		}
		if val < 3 {
			t.Errorf("too few executions: %d", val)
		}
		t.Logf("Total executions: %d", val)
	})

	t.Run("CancelTask", func(t *testing.T) {
		e := NewEventManager()
		var counter int32
		// Interval 50ms
		cancel := e.Add("cancel_task", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}, Every(50*time.Millisecond))

		e.Start()
		time.Sleep(120 * time.Millisecond) // Should run ~2 times (50, 100)
		cancel()
		time.Sleep(100 * time.Millisecond) // Should NOT run anymore
		e.Stop()

		val := atomic.LoadInt32(&counter)
		// It might run 2 or 3 times depending on exact timing, but definitely shouldn't run 4+ times.
		// If it wasn't cancelled, it would run 4-5 times total.
		if val > 3 {
			t.Errorf("task was not cancelled, ran %d times", val)
		}
		if val < 1 {
			t.Errorf("task did not run before cancel")
		}
	})

	t.Run("DistributedTask_NoRedis_Fallback", func(t *testing.T) {
		e := NewEventManager()
		// Explicitly unset Redis to test fallback behavior, as NewEventManager now provides a default memory Redis.
		e.SetRedis(nil)

		var counter int32
		e.Add("fallback_task", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}, Every(50*time.Millisecond), Distributed(time.Second))

		e.Start()
		time.Sleep(100 * time.Millisecond)
		e.Stop()

		if atomic.LoadInt32(&counter) == 0 {
			t.Error("expected fallback execution when redis is missing")
		}
	})

	t.Run("ManualRun", func(t *testing.T) {
		e := NewEventManager()
		var counter int32

		// One-time task
		e.Add("manual_task", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		})

		// 1. Run manually
		if err := e.Run("manual_task"); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if val := atomic.LoadInt32(&counter); val != 1 {
			t.Errorf("expected 1 execution, got %d", val)
		}

		// 2. Run again (should skip because it's one-time)
		if err := e.Run("manual_task"); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if val := atomic.LoadInt32(&counter); val != 1 {
			t.Errorf("expected 1 execution (skip second), got %d", val)
		}

		// Periodic task
		var pCounter int32
		e.Add("periodic_task", func() error {
			atomic.AddInt32(&pCounter, 1)
			return nil
		}, Every(time.Hour)) // Long interval

		// 3. Run manually
		if err := e.Run("periodic_task"); err != nil {
			t.Fatalf("Run periodic failed: %v", err)
		}
		if val := atomic.LoadInt32(&pCounter); val != 1 {
			t.Errorf("expected 1 execution, got %d", val)
		}

		// 4. Run again (should run again)
		if err := e.Run("periodic_task"); err != nil {
			t.Fatalf("Run periodic failed: %v", err)
		}
		if val := atomic.LoadInt32(&pCounter); val != 2 {
			t.Errorf("expected 2 executions, got %d", val)
		}
	})

	t.Run("ListAndClear", func(t *testing.T) {
		e := NewEventManager()
		e.SetRedis(rdb)

		e.Add("task1", func() error { return nil }, Distributed(time.Minute))
		e.Add("task2", func() error { return nil })

		// List
		keys := e.List()
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}

		// Verify keys present
		hasTask1 := false
		hasTask2 := false
		for _, k := range keys {
			if k == "task1" {
				hasTask1 = true
			}
			if k == "task2" {
				hasTask2 = true
			}
		}
		if !hasTask1 || !hasTask2 {
			t.Errorf("missing keys in List: %v", keys)
		}

		// Simulate lock for task1
		lockKey := "vigo:event:lock:task1"
		rdb.Set(context.Background(), lockKey, "locked", time.Minute)

		// Clear
		if err := e.Clear(); err != nil {
			t.Fatalf("Clear failed: %v", err)
		}

		// Verify lock gone
		exists, err := rdb.Exists(context.Background(), lockKey).Result()
		if err != nil {
			t.Fatalf("redis exists failed: %v", err)
		}
		if exists != 0 {
			t.Error("lock key should be deleted after Clear")
		}
	})

	t.Run("ExecutionOrder_After", func(t *testing.T) {
		e := NewEventManager()
		var order []string
		var mu sync.Mutex

		record := func(name string) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}

		// Task B
		e.Add("B", func() error {
			time.Sleep(50 * time.Millisecond) // Simulate work
			record("B")
			return nil
		})

		// Task A runs After B
		e.Add("A", func() error {
			record("A")
			return nil
		}, After("B"))

		e.Start()
		// Wait enough time for both to finish
		time.Sleep(200 * time.Millisecond)
		e.Stop()

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 2 {
			t.Fatalf("expected 2 tasks, got %d: %v", len(order), order)
		}
		if order[0] != "B" || order[1] != "A" {
			t.Errorf("expected order [B, A], got %v", order)
		}
	})

	t.Run("ExecutionOrder_Before", func(t *testing.T) {
		e := NewEventManager()
		var order []string
		var mu sync.Mutex

		record := func(name string) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}

		// Task A runs Before B
		// Note: We add A first. B doesn't exist yet.
		e.Add("A", func() error {
			time.Sleep(50 * time.Millisecond)
			record("A")
			return nil
		}, Before("B"))

		// Task B
		e.Add("B", func() error {
			record("B")
			return nil
		})

		e.Start()
		time.Sleep(200 * time.Millisecond)
		e.Stop()

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 2 {
			t.Fatalf("expected 2 tasks, got %d: %v", len(order), order)
		}
		if order[0] != "A" || order[1] != "B" {
			t.Errorf("expected order [A, B], got %v", order)
		}
	})

	t.Run("ExecutionOrder_FailureDoesNotBlock", func(t *testing.T) {
		e := NewEventManager()
		var executed []string
		var mu sync.Mutex

		record := func(name string) {
			mu.Lock()
			executed = append(executed, name)
			mu.Unlock()
		}

		// Task Fail
		e.Add("Fail", func() error {
			time.Sleep(50 * time.Millisecond)
			record("Fail")
			return errors.New("simulated failure")
		})

		// Task Dependent runs After Fail
		e.Add("Dependent", func() error {
			record("Dependent")
			return nil
		}, After("Fail"))

		e.Start()
		time.Sleep(200 * time.Millisecond)
		e.Stop()

		mu.Lock()
		defer mu.Unlock()
		if len(executed) != 2 {
			t.Fatalf("expected 2 tasks (even if first failed), got %d: %v", len(executed), executed)
		}
		if executed[0] != "Fail" || executed[1] != "Dependent" {
			t.Errorf("expected order [Fail, Dependent], got %v", executed)
		}
	})

	t.Run("ExecutionOrder_IgnoredForPeriodic", func(t *testing.T) {
		e := NewEventManager()
		var counter int32
		// Periodic task with After("non_existent")
		// Should ignore After and run anyway.
		e.Add("periodic", func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		}, Every(20*time.Millisecond), After("non_existent"))

		e.Start()
		time.Sleep(100 * time.Millisecond)
		e.Stop()

		val := atomic.LoadInt32(&counter)
		if val < 3 {
			t.Errorf("expected periodic task to run ignoring After, got %d runs", val)
		}
	})

	t.Run("SerialOrder", func(t *testing.T) {
		e := NewEventManager()
		var order []string
		var mu sync.Mutex

		record := func(name string) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}

		// Add tasks in order
		e.Add("1", func() error {
			time.Sleep(50 * time.Millisecond)
			record("1")
			return nil
		})
		e.Add("2", func() error {
			time.Sleep(10 * time.Millisecond) // Shorter task, would finish first if concurrent
			record("2")
			return nil
		})
		e.Add("3", func() error {
			record("3")
			return nil
		})

		e.Start()
		time.Sleep(200 * time.Millisecond)
		e.Stop()

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 3 {
			t.Fatalf("expected 3 tasks, got %d: %v", len(order), order)
		}
		if order[0] != "1" || order[1] != "2" || order[2] != "3" {
			t.Errorf("expected order [1, 2, 3], got %v", order)
		}
	})

	t.Run("AddAfterStart", func(t *testing.T) {
		e := NewEventManager()
		var order []string
		var mu sync.Mutex

		record := func(name string) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}

		e.Start()

		e.Add("1", func() error {
			time.Sleep(50 * time.Millisecond)
			record("1")
			return nil
		})
		e.Add("2", func() error {
			record("2")
			return nil
		})

		time.Sleep(200 * time.Millisecond)
		e.Stop()

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 2 {
			t.Fatalf("expected 2 tasks, got %d: %v", len(order), order)
		}
		if order[0] != "1" || order[1] != "2" {
			t.Errorf("expected order [1, 2], got %v", order)
		}
	})

	t.Run("MixedTypes", func(t *testing.T) {
		e := NewEventManager()
		var counter int32

		// Simple One-Time (Serial)
		e.Add("serial", func() error {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&counter, 1)
			return nil
		})

		// Periodic (Concurrent)
		e.Add("periodic", func() error {
			atomic.AddInt32(&counter, 10)
			return nil
		}, Every(20*time.Millisecond))

		// One-Time with Deps (Concurrent)
		e.Add("dep", func() error {
			atomic.AddInt32(&counter, 100)
			return nil
		}, After("serial"))

		e.Start()
		time.Sleep(150 * time.Millisecond)
		e.Stop()

		val := atomic.LoadInt32(&counter)
		// Expect:
		// Serial: +1 (runs once)
		// Periodic: +10 * ~7 times = ~70
		// Dep: +100 (runs after serial)
		// Total ~171

		if val < 111 {
			t.Errorf("expected at least 111 (1 serial + 1 dep + periodic), got %d", val)
		}
	})
}
