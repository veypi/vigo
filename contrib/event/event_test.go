package event

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	sharedcfg "github.com/veypi/vigo/contrib/config"
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
		e.Add("local_task", func(context.Context) error {
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
		e.Add("dist_task_1", func(context.Context) error {
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
		taskFn := func(context.Context) error {
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
		cancel := e.Add("cancel_task", func(context.Context) error {
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
		e.Add("fallback_task", func(context.Context) error {
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

	t.Run("DistributedTask_UsesSharedRedis", func(t *testing.T) {
		sharedcfg.SetSharedRedis(rdb)

		e1 := NewEventManager()
		e2 := NewEventManager()

		var counter int32
		taskFn := func(context.Context) error {
			atomic.AddInt32(&counter, 1)
			return nil
		}

		opts := []Option{Every(100 * time.Millisecond), Distributed(50 * time.Millisecond)}
		e1.Add("shared_default", taskFn, opts...)
		e2.Add("shared_default", taskFn, opts...)

		e1.Start()
		e2.Start()

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
		if val > 6 {
			t.Errorf("shared redis lock failed, too many executions: %d", val)
		}
		if val < 3 {
			t.Errorf("shared redis produced too few executions: %d", val)
		}
	})

	t.Run("ManualRun", func(t *testing.T) {
		e := NewEventManager()
		var counter int32

		// One-time task
		e.Add("manual_task", func(context.Context) error {
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
		e.Add("periodic_task", func(context.Context) error {
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

		e.Add("task1", func(context.Context) error { return nil }, Distributed(time.Minute))
		e.Add("task2", func(context.Context) error { return nil })

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
		e.Add("B", func(context.Context) error {
			time.Sleep(50 * time.Millisecond) // Simulate work
			record("B")
			return nil
		})

		// Task A runs After B
		e.Add("A", func(context.Context) error {
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
		e.Add("A", func(context.Context) error {
			time.Sleep(50 * time.Millisecond)
			record("A")
			return nil
		}, Before("B"))

		// Task B
		e.Add("B", func(context.Context) error {
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
		e.Add("Fail", func(context.Context) error {
			time.Sleep(50 * time.Millisecond)
			record("Fail")
			return errors.New("simulated failure")
		})

		// Task Dependent runs After Fail
		e.Add("Dependent", func(context.Context) error {
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
		e.Add("periodic", func(context.Context) error {
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
		e.Add("1", func(context.Context) error {
			time.Sleep(50 * time.Millisecond)
			record("1")
			return nil
		})
		e.Add("2", func(context.Context) error {
			time.Sleep(10 * time.Millisecond) // Shorter task, would finish first if concurrent
			record("2")
			return nil
		})
		e.Add("3", func(context.Context) error {
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

		e.Add("1", func(context.Context) error {
			time.Sleep(50 * time.Millisecond)
			record("1")
			return nil
		})
		e.Add("2", func(context.Context) error {
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
		e.Add("serial", func(context.Context) error {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&counter, 1)
			return nil
		})

		// Periodic (Concurrent)
		e.Add("periodic", func(context.Context) error {
			atomic.AddInt32(&counter, 10)
			return nil
		}, Every(20*time.Millisecond))

		// One-Time with Deps (Concurrent)
		e.Add("dep", func(context.Context) error {
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

	t.Run("StopDoesNotDeadlock", func(t *testing.T) {
		e := NewEventManager()
		e.Add("first", func(context.Context) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		})
		e.Add("second", func(context.Context) error {
			return nil
		}, After("first"))

		e.Start()

		done := make(chan struct{})
		go func() {
			e.Stop()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Stop blocked unexpectedly")
		}
	})
}

// ---- 分布式锁生命周期（2026-10-08 重写：token + 续约 + 完成即释放 + done 标记）----

// one-time 任务完成即释放：锁 key 立即消失，崩溃接管/重跑不必等 TTL。
func TestDistributedLockReleasedOnCompletion(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("release_task", func(context.Context) error { return nil }, Distributed(10*time.Minute))

	if err := e.Run("release_task"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("vigo:event:lock:release_task") {
		t.Fatal("one-time lock should be released immediately after completion")
	}
	if !s.Exists("vigo:event:done:release_task") {
		t.Fatal("one-time success must leave a done marker")
	}
}

// 周期任务不释放锁：锁要活到下一个 tick，这是「每周期集群一次」的载体
// （完成即释放会让每个节点的 tick 各跑一遍，见 TestDistributedPeriodicDedupWithOffsetNodes）。
func TestDistributedPeriodicLockRetainedUntilTTL(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("retain_task", func(context.Context) error { return nil }, Every(time.Hour), Distributed(time.Minute))

	if err := e.Run("retain_task"); err != nil {
		t.Fatal(err)
	}
	if !s.Exists("vigo:event:lock:retain_task") {
		t.Fatal("periodic task must keep the lock until its TTL expires")
	}
	if s.Exists("vigo:event:done:retain_task") {
		t.Fatal("periodic tasks must not write a done marker")
	}
}

// 周期分布式任务跨节点去重（2026-10-08 回归）：两节点 tick 相位错开半个周期时，
// 每周期仍只执行一次。旧实现（周期任务完成即释放）实测 1s 内跑 10 次（2 节点×5 tick），
// 现在应为 ~5 次。
func TestDistributedPeriodicDedupWithOffsetNodes(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	var counter int32
	fn := func(context.Context) error { atomic.AddInt32(&counter, 1); return nil }
	opts := []Option{Every(200 * time.Millisecond), Distributed(0)} // 0 → 默认 TTL=interval

	e1 := NewEventManager()
	e1.SetRedis(rdb)
	e1.Add("periodic_shared", fn, opts...)
	e2 := NewEventManager()
	e2.SetRedis(rdb)
	e2.Add("periodic_shared", fn, opts...)

	e1.Start()
	time.Sleep(100 * time.Millisecond) // 相位差 = 半周期
	e2.Start()

	// miniredis 的时钟是虚拟的（不会自己走），跟真实睡眠同步推进 TTL。
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
	time.Sleep(time.Second)
	close(done)
	e1.Stop()
	e2.Stop()

	val := atomic.LoadInt32(&counter)
	if val > 7 {
		t.Fatalf("periodic dedup broken: %d executions in ~1s with Every(200ms) (2 nodes would be ~10)", val)
	}
	if val < 3 {
		t.Fatalf("too few executions: %d", val)
	}
}

// one-time 分布式任务：done 标记集群级去重——本节点执行后，其他节点不再执行。
func TestDistributedOneTimeDoneMarker(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	var counter int32
	e1 := NewEventManager()
	e1.SetRedis(rdb)
	e2 := NewEventManager()
	e2.SetRedis(rdb)
	fn := func(context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}
	e1.Add("once_task", fn, Distributed(time.Minute))
	e2.Add("once_task", fn, Distributed(time.Minute))

	if err := e1.Run("once_task"); err != nil {
		t.Fatal(err)
	}
	if !s.Exists("vigo:event:done:once_task") {
		t.Fatal("done marker should be set after success")
	}
	// 另一节点（无本地 executed 记录）Run：被 done 标记拦下
	if err := e2.Run("once_task"); err != nil {
		t.Fatal(err)
	}
	if val := atomic.LoadInt32(&counter); val != 1 {
		t.Fatalf("one-time task ran %d times, want 1", val)
	}

	// 失败不写 done 标记：他节点可补跑（at-least-once）
	e1.Add("fail_once", func(context.Context) error { return errors.New("boom") }, Distributed(time.Minute))
	if err := e1.Run("fail_once"); err == nil {
		t.Fatal("want error")
	}
	if s.Exists("vigo:event:done:fail_once") {
		t.Fatal("done marker must not be set on failure")
	}
}

// 续约保护长任务：任务执行超过 TTL 时锁不释放（无续约的旧实现会过期→双活）。
// 真实时间 + miniredis（不走 FastForward）：TTL 200ms，任务跑 500ms，
// 期间另一节点抢锁应失败。
func TestDistributedRenewalProtectsLongTask(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	started := make(chan struct{})
	finish := make(chan struct{})
	e1 := NewEventManager()
	e1.SetRedis(rdb)
	e1.Add("long_task", func(ctx context.Context) error {
		close(started)
		select {
		case <-finish:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}, Every(50*time.Millisecond), Distributed(200*time.Millisecond))
	defer e1.Stop()
	e1.Start()

	<-started
	// 等过 TTL 两倍时长（无续约锁早已过期），另一节点手动 Run 应抢不到锁
	time.Sleep(400 * time.Millisecond)
	e2 := NewEventManager()
	e2.SetRedis(rdb)
	var ran2 int32
	e2.Add("long_task", func(context.Context) error {
		atomic.AddInt32(&ran2, 1)
		return nil
	}, Distributed(200*time.Millisecond))
	if err := e2.Run("long_task"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&ran2) != 0 {
		t.Fatal("second node executed while first node still holds the (renewed) lock")
	}
	close(finish)
}

// 锁易主（续约发现 token 不匹配）→ 任务 ctx 被取消，防双活。
func TestDistributedLockLostCancelsTask(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	ctxCanceled := make(chan struct{})
	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("victim_task", func(ctx context.Context) error {
		<-ctx.Done()
		close(ctxCanceled)
		return ctx.Err()
	}, Every(50*time.Millisecond), Distributed(100*time.Millisecond))
	e.Start()
	defer e.Stop()

	// 等任务持锁运行，然后模拟锁被夺走（他节点/Clear 误删后重建）：直接覆写成
	// 别人的 token（Del 非必需，Set 已覆盖）。
	time.Sleep(120 * time.Millisecond)
	_ = rdb.Set(context.Background(), "vigo:event:lock:victim_task", "other-token", time.Minute).Err()

	select {
	case <-ctxCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("task ctx was not canceled after lock loss")
	}
}

// panic 转为非 nil error（Run 调用方可见），且进程存活。
func TestPanicReturnsError(t *testing.T) {
	e := NewEventManager()
	e.Add("panic_task", func(context.Context) error { panic("boom") })
	err := e.Run("panic_task")
	if err == nil {
		t.Fatal("panic should surface as error")
	}
	if got := err.Error(); !strings.Contains(got, "panic") || !strings.Contains(got, "boom") {
		t.Fatalf("error = %q, want panic text", got)
	}
	// panic 后 executed 已消费、done chan 已关闭（Run 路径 err==nil 才 markDone——
	// panic 是 error，不标 done；本断言钉住的是「不标 done」语义）
	if err := e.Run("panic_task"); err != nil {
		t.Fatalf("second Run should be skipped (executed), got %v", err)
	}
}

// 周期任务执行期续约不得把锁推出 tick 边界（2026-10-10 回归）：
// 任务时长 = interval/2（> ttl/3，必然触发一次续约）时，续约会把过期时刻推到
// 「末次续约 + ttl」，晚于本周期结束（start + interval）→ 下一个 tick 被自己的锁
// 跳掉（实测 1.2s 内只剩 4 次）。把剩余寿命钉回周期边界后应为 ~6 次。
func TestDistributedPeriodicSlowTaskDoesNotSkipPeriod(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	var counter int32
	fn := func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		select {
		case <-time.After(100 * time.Millisecond): // 半个周期：续约会发生一次
		case <-ctx.Done():
		}
		return nil
	}
	opts := []Option{Every(200 * time.Millisecond), Distributed(0)} // 0 → 默认 TTL=interval

	e1 := NewEventManager()
	e1.SetRedis(rdb)
	e1.Add("periodic_slow", fn, opts...)
	e2 := NewEventManager()
	e2.SetRedis(rdb)
	e2.Add("periodic_slow", fn, opts...)

	e1.Start()
	time.Sleep(100 * time.Millisecond) // 相位差 = 半周期
	e2.Start()

	// miniredis 的时钟是虚拟的（不会自己走），跟真实睡眠同步推进 TTL。
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
	time.Sleep(1200 * time.Millisecond)
	close(done)
	e1.Stop()
	e2.Stop()

	val := atomic.LoadInt32(&counter)
	if val < 5 {
		t.Fatalf("periodic cadence broken: %d executions in ~1.2s (Every(200ms), task 100ms) — lock outlived the tick boundary (ideal ~6, un-pinned ~4)", val)
	}
	if val > 8 {
		t.Fatalf("too many executions: %d", val)
	}
}

// cmdOrderHook 记录客户端侧的命令顺序（顺序类断言用，不依赖时序竞态）。
type cmdOrderHook struct {
	mu  sync.Mutex
	seq []string
}

func (h *cmdOrderHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *cmdOrderHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.record(cmd)
		return next(ctx, cmd)
	}
}

func (h *cmdOrderHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}

func (h *cmdOrderHook) record(cmd redis.Cmder) {
	args := cmd.Args()
	if len(args) < 2 {
		return
	}
	second, _ := args[1].(string)
	switch strings.ToLower(cmd.Name()) {
	case "set":
		switch {
		case strings.HasPrefix(second, doneKeyPrefix):
			h.append("done")
		case strings.HasPrefix(second, lockKeyPrefix):
			h.append("lock")
		}
	case "eval":
		switch {
		case strings.Contains(second, `call("del"`):
			h.append("release")
		case strings.Contains(second, `call("pexpire"`):
			h.append("extend")
		}
	}
}

func (h *cmdOrderHook) append(op string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq = append(h.seq, op)
}

func (h *cmdOrderHook) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.seq...)
}

// done 标记必须先落、再释放锁（2026-10-10 修正顺序）：反序会留下「锁空闲但 done
// 未落」的窗口（Exists→SetNX→Exists 三次往返量级），他节点抢到锁后查 done 为空，
// 会把同一个 one-time 任务再跑一遍。顺序用客户端 hook 断言，不靠时序碰运气。
func TestDistributedOneTimeWritesDoneBeforeReleasingLock(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	h := &cmdOrderHook{}
	rdb.AddHook(h)

	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("ordered_once", func(context.Context) error { return nil }, Distributed(time.Minute))

	if err := e.Run("ordered_once"); err != nil {
		t.Fatal(err)
	}

	seq := h.snapshot()
	iDone, iRelease := -1, -1
	for i, op := range seq {
		if op == "done" && iDone < 0 {
			iDone = i
		}
		if op == "release" && iRelease < 0 {
			iRelease = i
		}
	}
	if iDone < 0 || iRelease < 0 {
		t.Fatalf("cmd sequence %v: want both done-marker set and lock release", seq)
	}
	if iDone > iRelease {
		t.Fatalf("done marker written after lock release (sequence %v) — a peer node can grab the free lock, see no done marker and run the one-time task again", seq)
	}
	if !s.Exists("vigo:event:done:ordered_once") || s.Exists("vigo:event:lock:ordered_once") {
		t.Fatal("final state: done marker set, lock released")
	}
}

// daemon（RestartOnFail）分布式任务返回即释放锁：失败后的 1s 退避重试必须能重新
// 拿锁。回归：2026-10-10 前 daemon 不释放锁，重试时 SetNX 撞见自己上次残留的锁
// → executeTask 返 nil（被当成「他节点持有，跳过」）→ daemon 循环按「成功完成」
// 退出——任务恒失败时 6s 只跑了 1 次，RestartOnFail 名存实亡。
func TestDistributedDaemonReleasesLockAndRestarts(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	var counter int32
	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("daemon_restart", func(context.Context) error {
		atomic.AddInt32(&counter, 1)
		return errors.New("boom") // 恒失败：RestartOnFail 应持续退避重跑
	}, RestartOnFail(), Distributed(time.Minute))
	e.Start()
	defer e.Stop()

	// 失败 → 1s 退避 → 重跑：3.5s 内应 ≥3 次；未修时恒为 1（循环已退出）。
	time.Sleep(3500 * time.Millisecond)
	if got := atomic.LoadInt32(&counter); got < 3 {
		t.Fatalf("daemon ran %d times in 3.5s, want >= 3 — stale self-held lock made the retry look like a peer holds it and the daemon loop exited as if successful", got)
	}
}

// daemon 成功返回同样释放锁：成功 = 任务终结，锁不应拖到 TTL 才消失。
func TestDistributedDaemonSuccessReleasesLock(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})

	var counter int32
	e := NewEventManager()
	e.SetRedis(rdb)
	e.Add("daemon_once", func(context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}, RestartOnFail(), Distributed(time.Minute))
	e.Start()
	defer e.Stop()

	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Fatalf("daemon ran %d times, want exactly 1 (success ends the task)", got)
	}
	if s.Exists("vigo:event:lock:daemon_once") {
		t.Fatal("lock still held after daemon succeeded — it should be released on return, not linger until TTL")
	}
}
