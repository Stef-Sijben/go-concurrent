package lru

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/golang-lru/simplelru"
	cmap "github.com/orcaman/concurrent-map"
)

//	"github.com/hashicorp/golang-lru/blob/master/simplelru"

// LRU is a thread-safe least-recently used cache
type LRU struct {
	capacity int
	len      int64              // Fixed size because of atomic access
	items    cmap.ConcurrentMap // TODO: This only accepts string keys because of hashing
	evict    *list
	onEvict  simplelru.EvictCallback
	cleanup  sync.Cond
}

// Item is the value type of an LRU.items map
type item struct {
	key          string
	value        interface{}
	evictElement *element
}

// New creates an LRU of the given size.
func New(size int) (*LRU, error) {
	return NewWithEvict(size, nil)
}

// NewWithEvict returns an initialized empty LRU cache with an eviction callback
func NewWithEvict(size int, onEvict simplelru.EvictCallback) (*LRU, error) {
	if size <= 0 {
		return nil, errors.New("must provide a positive size")
	}

	c := &LRU{
		capacity: size,
		len:      0,
		items:    cmap.New(),
		evict:    newList(),
		onEvict:  onEvict,
		cleanup:  *sync.NewCond(new(sync.Mutex)),
	}

	go c.cleanupWorker() // always run a cleanup worker in the background
	return c, nil
}

func (c *LRU) cleanupWorker() {
	c.cleanup.L.Lock()
	defer c.cleanup.L.Unlock()

	for {
		for n := c.Len(); n > c.capacity; n = c.Len() {
			// Claim one eviction by decrementing the counter,
			// then we can allow other workers to test and claim
			atomic.AddInt64(&c.len, -1)
			c.cleanup.L.Unlock()

			popElement := c.evict.PopBack()
			if popElement == nil {
				// Pop failed; return claimed eviction, retry later
				atomic.AddInt64(&c.len, -1)
			} else {
				popItem := popElement.Value.(*item)
				c.items.RemoveCb(popItem.key,
					func(key string, v interface{}, exists bool) bool {
						// Check that the map entry was not replaced in the meantime
						if !exists {
							return false
						}
						return v.(*item) == popItem
					})
				if c.onEvict != nil {
					c.onEvict(popItem.key, popItem.value)
				}
			}

			c.cleanup.L.Lock()
		}
		// nothing to clean for now
		c.cleanup.Wait() // condition is checked in inner loop
	}
}

// Add inserts a value to the cache, returns true if an eviction
// occurred and updates the "recently used"-ness of the key.
func (c *LRU) Add(key, value interface{}) bool {
	keyStr, ok := key.(string)
	if !ok {
		return false // TODO: Report error, but interface does not have it
	}

	v := c.items.Upsert(keyStr, value,
		func(exist bool, valueInMap, newValue interface{}) interface{} {
			if exist {
				// Update existing node
				v := valueInMap.(item)
				// If the move to front fails, the item is being evicted,
				// so insert a new item instead.
				if c.evict.MoveToFront(v.evictElement) {
					v.value = newValue
					return &v
				}
			}

			// Create new node
			v := item{
				key:          keyStr,
				value:        newValue,
				evictElement: nil,
			}
			return &v
		}).(*item)
	if v.evictElement == nil {
		// new element inserted, count it and add to evict list
		c.cleanup.L.Lock()
		defer c.cleanup.L.Unlock()
		n := int(atomic.AddInt64(&c.len, 1))
		v.evictElement = c.evict.PushFront(v)
		if n > c.capacity {
			// actual cleanup happens in the background
			c.cleanup.Signal()
			return n > c.capacity
		}
	}

	return false
}

// Get returns key's value from the cache and
// updates the "recently used"-ness of the key. #value, isFound
func (c *LRU) Get(key interface{}) (value interface{}, ok bool) {
	keyStr, ok := key.(string)
	if ok {
		mapEntry, ok := c.items.Get(keyStr)
		if ok {
			mapItem, ok := mapEntry.(*item)
			if ok && c.evict.MoveToFront(mapItem.evictElement) {
				return mapItem.value, ok
			}
		}
	}
	return nil, false
}

// // Checks if a key exists in cache without updating the recent-ness.
// Contains(key interface{}) (ok bool)

// // Returns key's value without updating the "recently used"-ness of the key.
// Peek(key interface{}) (value interface{}, ok bool)

// // Removes a key from the cache.
// Remove(key interface{}) bool

// // Removes the oldest entry from cache.
// RemoveOldest() (interface{}, interface{}, bool)

// // Returns the oldest entry from the cache. #key, value, isFound
// GetOldest() (interface{}, interface{}, bool)

// // Returns a slice of the keys in the cache, from oldest to newest.
// Keys() []interface{}

// Len returns the number of items in the cache.
func (c *LRU) Len() int {
	return int(atomic.LoadInt64(&c.len))
}

// // Clears all cache entries.
// Purge()

// // Resizes cache, returning number evicted
// Resize(int) int
