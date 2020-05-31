package lru

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/golang-lru/simplelru"
	cmap "github.com/orcaman/concurrent-map"
)

// LRU is a thread-safe least-recently used cache
type LRU struct {
	capacity int
	len      int64              // Fixed size because of atomic access
	items    cmap.ConcurrentMap // TODO: This only accepts string keys because of hashing
	evict    *list
	onEvict  simplelru.EvictCallback
	cleanup  sync.Cond
	workers  sync.WaitGroup
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

	c.workers.Add(1)
	go c.cleanupWorker() // always run a cleanup worker in the background
	return c, nil
}

// Close releases the resources used by an LRU cache
func (c *LRU) Close() {
	// Causes the cleanup workers to remove all entries, then exit
	c.cleanup.L.Lock()
	c.capacity = 0
	c.cleanup.Broadcast()
	c.cleanup.L.Unlock()

	c.evict.Close()

	// Return only when all workers are stopped
	c.workers.Wait()
}

func (c *LRU) cleanupWorker() {
	defer c.workers.Done()
	c.cleanup.L.Lock()
	defer c.cleanup.L.Unlock()

	for {
		c.cleanup.L.Unlock()

		// Under heavy load, operate lock free (at least for the cleanup mutex)
		for n := c.Len(); n > c.capacity; n = c.Len() {
			// Claim one eviction by decrementing the counter
			if !atomic.CompareAndSwapInt64(&c.len, int64(n), int64(n-1)) {
				continue // Claim failed, try again
			}

			popElement := c.evict.PopBack()
			if popElement == nil {
				// Pop failed; return claimed eviction, try again
				atomic.AddInt64(&c.len, 1)
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
				popElement.Value = nil
				popItem.evictElement = nil
			}

		}

		// Perform one final check under lock before we go to sleep or exit
		c.cleanup.L.Lock()
		if c.Len() > c.capacity {
			continue // Someone inserted something before we locked, carry on
		} else if c.capacity > 0 {
			// Wait for something to clean up
			c.cleanup.Wait()
		} else {
			// Capacity is set to 0 in Close()
			return
		}
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
				// TODO: I think it would be better if the items were immutable
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
		n := int(atomic.AddInt64(&c.len, 1))
		c.cleanup.L.Unlock()
		v.evictElement = c.evict.PushFront(v)
		if n > c.capacity {
			// actual cleanup happens in the background
			c.cleanup.Signal()
			return true
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

// Contains checks if a key exists in cache without updating the recent-ness.
func (c *LRU) Contains(key interface{}) (ok bool) {
	keyStr, ok := key.(string)
	if ok {
		_, ok := c.items.Get(keyStr)
		return ok
	}
	return false
}

// Peek returns key's value without updating the "recently used"-ness of the key.
func (c *LRU) Peek(key interface{}) (value interface{}, ok bool) {
	keyStr, ok := key.(string)
	if ok {
		mapEntry, ok := c.items.Get(keyStr)
		if ok {
			return mapEntry.(*item).value, true
		}
	}
	return nil, false
}

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
