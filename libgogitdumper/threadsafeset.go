package libgogitdumper

import "sync"

type ThreadSafeSet struct {
	mutex *sync.RWMutex
	vals  map[string]bool
}

func (t ThreadSafeSet) Init() ThreadSafeSet {
	t = ThreadSafeSet{}
	t.mutex = &sync.RWMutex{}
	t.vals = make(map[string]bool)
	return t
}

func (t ThreadSafeSet) HasValue(s string) bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if _, ok := t.vals[s]; ok {
		return true
	}
	return false
}

func (t *ThreadSafeSet) Add(s string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.vals[s] = true

}
