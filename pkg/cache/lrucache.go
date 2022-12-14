// Ported from stargz-snapshotter, copyright The stargz-snapshotter Authors.
// https://github.com/containerd/stargz-snapshotter/blob/38baee48ed29150906d23edba64362414dce265d/util/cacheutil/lrucache.go
package cache

import (
	"sync"

	"github.com/golang/groupcache/lru"
)

// LRUCache is "groupcache/lru"-like cache. The difference is that "groupcache/lru" immediately
// finalizes theevicted contents using OnEvicted callback but our version strictly tracks the
// reference counts of contents and calls OnEvicted when nobody refers to the evicted contents.
type LRUCache struct {
	cache *lru.Cache
	mu    sync.Mutex

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	OnEvicted func(key string, value interface{})
}

// NewLRUCache creates new lru cache.
func NewLRUCache(maxEntries int) *LRUCache {
	inner := lru.New(maxEntries)
	inner.OnEvicted = func(key lru.Key, value interface{}) {
		// Decrease the ref count incremented in Add().
		// When nobody refers to this value, this value will be finalized via refCounter.
		value.(*refCounter).finalize()
	}
	return &LRUCache{
		cache: inner,
	}
}

// Get retrieves the specified object from the cache and increments the reference counter of the
// target content. Client must call `done` callback to decrease the reference count when the value
// will no longer be used.
func (c *LRUCache) Get(key string) (value interface{}, done func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	o, ok := c.cache.Get(key)
	if !ok {
		return nil, nil, false
	}
	rc := o.(*refCounter)
	rc.inc()
	return rc.v, c.decreaseOnceFunc(rc), true
}

// Add adds object to the cache and returns the cached contents with incrementing the reference count.
// If the specified content already exists in the cache, this sets `added` to false and returns
// "already cached" content (i.e. doesn't replace the content with the new one). Client must call
// `done` callback to decrease the counter when the value will no longer be used.
func (c *LRUCache) Add(key string, value interface{}) (cachedValue interface{}, done func(), added bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if o, ok := c.cache.Get(key); ok {
		rc := o.(*refCounter)
		rc.inc()
		return rc.v, c.decreaseOnceFunc(rc), false
	}
	rc := &refCounter{
		key:       key,
		v:         value,
		onEvicted: c.OnEvicted,
	}
	rc.initialize() // Keep this object having at least 1 ref count (will be decreased in OnEviction)
	rc.inc()        // The client references this object (will be decreased on "done")
	c.cache.Add(key, rc)
	return rc.v, c.decreaseOnceFunc(rc), true
}

// Remove removes the specified contents from the cache. OnEvicted callback will be called when
// nobody refers to the removed content.
func (c *LRUCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Remove(key)
}

func (c *LRUCache) decreaseOnceFunc(rc *refCounter) func() {
	var once sync.Once
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		once.Do(func() { rc.dec() })
	}
}

type refCounter struct {
	onEvicted func(key string, value interface{})

	key       string
	v         interface{}
	refCounts int64

	mu sync.Mutex

	initializeOnce sync.Once
	finalizeOnce   sync.Once
}

func (r *refCounter) inc() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refCounts++
}

func (r *refCounter) dec() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refCounts--
	if r.refCounts <= 0 && r.onEvicted != nil {
		// nobody will refer this object
		r.onEvicted(r.key, r.v)
	}
}

func (r *refCounter) initialize() {
	r.initializeOnce.Do(func() { r.inc() })
}

func (r *refCounter) finalize() {
	r.finalizeOnce.Do(func() { r.dec() })
}
