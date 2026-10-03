// stringpool provides a pool of string pointers, ensuring that each string is
// stored only once in memory. This is useful for queries that have many
// results, as the amount of source packages is limited. So, as soon as
// len(results) > len(sourcepackages), you save memory using a stringpool.
package stringpool

import "sync"

type StringPool struct {
	sync.RWMutex
	strings []string
	indexes map[string]uint32
}

func NewStringPool() *StringPool {
	return &StringPool{
		indexes: make(map[string]uint32),
	}
}

func (pool *StringPool) Intern(s string) uint32 {
	// Check if the entry is already in the pool with a slightly cheaper
	// (read-only) mutex.
	pool.RLock()
	idx, ok := pool.indexes[s]
	pool.RUnlock()
	if ok {
		return idx
	}

	pool.Lock()
	defer pool.Unlock()
	// Check again in case another goroutine acquired the lock first.
	idx, ok = pool.indexes[s]
	if ok {
		return idx
	}
	idx = uint32(len(pool.strings))
	pool.strings = append(pool.strings, s)
	pool.indexes[s] = idx
	return idx
}

func (pool *StringPool) Get(idx uint32) string {
	pool.RLock()
	defer pool.RUnlock()
	return pool.strings[idx]
}
