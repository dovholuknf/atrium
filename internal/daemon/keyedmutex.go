package daemon

import (
	"sort"
	"strings"
	"sync"
)

// keyedMutex serializes work that shares a key.
//
// It is the lock that makes a launch's check-then-spawn region atomic against
// another caller. `launch.go` reads whether a runner is live on a card, or
// whether a conversation is already resumed somewhere, and only afterwards
// spawns and registers the process that would change those answers. Two callers
// racing through that gap both see "not live" and both spawn onto the one
// conversation, which is the braided-transcript corruption launch.go's own
// comments warn about. Holding this across the gap closes it.
//
// A mutex is created on first use and kept. The keys are card ids and resume
// ids, a bounded set, so a handful of idle mutexes left behind cost nothing and
// buy a design with no reference counting to get wrong.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: map[string]*sync.Mutex{}}
}

// get returns the mutex for a key, making it on first use.
func (k *keyedMutex) get(key string) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		k.locks[key] = m
	}
	return m
}

// lock takes every key's mutex and returns the release.
//
// SORTED AND DEDUPED, which is the whole reason a caller passes the set at once
// rather than locking one key and then another. Two callers asking for the same
// two keys in the opposite order would deadlock, each holding the one the other
// wants; a single sorted acquisition order removes that. An empty set is a
// valid no-op, so a plain launch that names neither a card nor a resume is not
// serialized against anything.
func (k *keyedMutex) lock(keys ...string) func() {
	seen := map[string]bool{}
	uniq := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, key)
	}
	sort.Strings(uniq)

	held := make([]*sync.Mutex, 0, len(uniq))
	for _, key := range uniq {
		m := k.get(key)
		m.Lock()
		held = append(held, m)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
}

// launchTaskKey and launchResumeKey namespace the two kinds of key so a card id
// and a resume id that happen to be the same string cannot collide on one lock.
func launchTaskKey(taskID string) string     { return "task:" + taskID }
func launchResumeKey(resume string) string    { return "resume:" + resume }

// launchKeys is the set of locks a launch or restart must hold across its
// check-then-spawn region: the card it lands on, and the conversation it
// resumes. Either may be empty, which drops out of the set.
func launchKeys(taskID, resume string) []string {
	var keys []string
	if id := strings.TrimSpace(taskID); id != "" {
		keys = append(keys, launchTaskKey(id))
	}
	if r := strings.TrimSpace(resume); r != "" {
		keys = append(keys, launchResumeKey(r))
	}
	return keys
}
