// stringpool provides a pool of string pointers, ensuring that each string is
// stored only once in memory. This is useful for queries that have many
// results, as the amount of source packages is limited. So, as soon as
// len(results) > len(sourcepackages), you save memory using a stringpool.
package stringpool

import (
	"sync"
)

type StringPool struct {
	sync.RWMutex
	strings map[string]*string
}

func NewStringPool() *StringPool {
	return &StringPool{
		strings: make(map[string]*string)}
}

func (pool *StringPool) Get(s string) *string {
	// Check if the entry is already in the pool with a slightly cheaper
	// (read-only) mutex.
	pool.RLock()
	stored, ok := pool.strings[s]
	pool.RUnlock()
	if ok {
		return stored
	}

	pool.Lock()
	defer pool.Unlock()
	stored, ok = pool.strings[s]
	if ok {
		return stored
	}
	pool.strings[s] = &s
	return &s
}
