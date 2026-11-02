//go:build integration

package small

import (
	"sync"
	"testing"
	"time"
)

// Two callers sharing a key run one at a time. This is the whole point: it is
// the mutual exclusion that makes a launch's check-then-spawn region atomic.
func TestKeyedMutexSerializesSameKey(t *testing.T) {
	t.Parallel()
	k := NewKeyedMutex()

	unlock := k.Lock("task:one")
	ran := make(chan struct{})
	go func() {
		u := k.Lock("task:one")
		close(ran)
		u()
	}()

	select {
	case <-ran:
		t.Fatal("a second locker of the same key ran while the first held it")
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("the second locker never ran after the first released")
	}
}

// Different keys do not block each other, so two unrelated launches are not
// dragged into a queue behind one another.
func TestKeyedMutexAllowsDifferentKeys(t *testing.T) {
	t.Parallel()
	k := NewKeyedMutex()

	unlock := k.Lock("task:one")
	defer unlock()

	ran := make(chan struct{})
	go func() {
		u := k.Lock("task:two")
		close(ran)
		u()
	}()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("a lock on a different key blocked behind an unrelated one")
	}
}

// Two callers asking for the same two keys in the opposite order must not
// deadlock. The sorted acquisition order is what stops each from holding the one
// the other wants.
func TestKeyedMutexMultiKeyNoDeadlock(t *testing.T) {
	t.Parallel()
	k := NewKeyedMutex()

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 200; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				u := k.Lock("task:a", "resume:b")
				u()
			}()
			go func() {
				defer wg.Done()
				// The other order. lock sorts, so both take a then b.
				u := k.Lock("resume:b", "task:a")
				u()
			}()
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("locking the same two keys in opposite orders deadlocked")
	}
}

// An empty set is a valid no-op, so a plain launch that names neither a card nor
// a resume is serialized against nothing.
func TestKeyedMutexEmptyIsANoOp(t *testing.T) {
	t.Parallel()
	k := NewKeyedMutex()
	unlock := k.Lock(LaunchKeys("", "")...)
	unlock()

	ran := make(chan struct{})
	go func() {
		u := k.Lock(LaunchKeys("", "")...)
		close(ran)
		u()
	}()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("an empty key set serialized two callers")
	}
}
