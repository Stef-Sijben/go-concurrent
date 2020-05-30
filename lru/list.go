// Concurrent doubly-linked list optimised for use in LRU caching.
// It only supports PushFront, MoveToFront and PopBack.
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lru

import (
	"sync"
	"sync/atomic"
)

// Element is an element of a linked list.
type element struct {
	// Next and previous pointers in the doubly-linked list of elements.
	next, prev *element

	// The list to which this element belongs.
	list *list

	// A mutex protects all accesses to the list
	mutex sync.Mutex

	// The value stored with this element.
	Value interface{}
}

// list is a doubly linked list optimised for LRU caches and concurrent access.
// Note that insertions (including those in MoveToFront) are asynchronous,
// so the order of elements may vary slightly under load, and you may not
// immediately see the inserted element in the list.
type list struct {
	// Separate sentinels avoid contention between operations at either end
	head, tail element

	// Fixed size because of atomic access
	len               int64
	pendingInsertions int64 // Count async insertions waiting to be performed
}

// New returns an initialized list. Always create LRUList through New().
func newList() *list {
	l := new(list)
	l.len = 0
	l.pendingInsertions = 0

	l.head.prev = nil
	l.head.list = l
	l.head.next = &l.tail

	l.tail.next = nil
	l.tail.list = l
	l.tail.prev = &l.head

	return l
}

// Len returns the number of elements of list l.
// The complexity is O(1).
func (l *list) Len() int { return int(atomic.LoadInt64(&l.len)) }

// insertFront asynchronously inserts e at the front of l.
func (l *list) insertFront(e *element) {
	h := &l.head
	h.mutex.Lock()
	defer h.mutex.Unlock()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	n := h.next
	n.mutex.Lock()
	defer n.mutex.Unlock()

	h.next = e
	e.prev = h
	e.next = n
	n.prev = e
	e.list = l

	atomic.AddInt64(&l.pendingInsertions, -1)
}

// Returns the predecessor of e in l in a thread safe way.
// The returned element, if not nil, is locked for writing.
func predecessor(e *element) *element {
	e.mutex.Lock()
	for p := e.prev; p != nil; p = e.prev {
		// We must unlock here to avoid deadlock: Always lock head-to-tail
		e.mutex.Unlock()
		p.mutex.Lock()
		if p.next == e {
			return p
		}
		// We got a new predecessor before we got the lock, try again
		p.mutex.Unlock()
		e.mutex.Lock()
	}
	// If the loop terminates without returning, e was removed from l
	e.mutex.Unlock()
	return nil
}

// remove removes e from its list, decrements its len if appropriate.
// e.list is set to newList iff this call removed it.
// Returns e and whether this call removed it.
func (l *list) remove(e *element, validateList bool, newList *list) (*element, bool) {
	p := predecessor(e)
	if p == nil {
		// Someone else already deleted e for us, we're done
		return e, false
	}
	defer p.mutex.Unlock()
	e.mutex.Lock()
	if validateList && e.list != l {
		return e, false
	}
	defer e.mutex.Unlock()
	n := e.next
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if newList != e.list {
		atomic.AddInt64(&e.list.len, -1)
		e.list = newList
		if newList != nil {
			atomic.AddInt64(&e.list.len, 1)
		}
	}

	p.next = n
	n.prev = p
	e.next = nil
	e.prev = nil
	e.list = newList
	return e, true
}

// PopBack removes the last element from l if l is not empty.
// It returns the element value e.Value.
func (l *list) PopBack() *element {
	e := predecessor(&l.tail)
	e.mutex.Unlock()
	if e == &l.head {
		return nil // list empty. Note: async insertions can still be pending
	}
	if _, ok := l.remove(e, true, nil); ok {
		return e
	}
	return nil
}

// PushFront inserts a new element e with value v at the front of list l and returns e.
func (l *list) PushFront(v interface{}) *element {
	e := &element{Value: v}
	atomic.AddInt64(&l.len, 1)
	atomic.AddInt64(&l.pendingInsertions, 1)
	go l.insertFront(e)
	return e
}

// MoveToFront moves element e to the front of list l.
// It is allowed to move an element not in l through MoveToFront().
// The element must not be nil.
func (l *list) MoveToFront(e *element) bool {
	_, ok := l.remove(e, false, l)
	if ok {
		atomic.AddInt64(&l.pendingInsertions, 1)
		go l.insertFront(e)
		return true
	}
	// If someone else is already moving e to front of l, that's also fine
	return e.list == l
}
