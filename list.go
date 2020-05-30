// Concurrent doubly-linked list based on container.List.
// Code heavily inspired by the original.
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package concurrent

import (
	"sync"
	"sync/atomic"
)

// Element is an element of a linked list.
type Element struct {
	// Next and previous pointers in the doubly-linked list of elements.
	next, prev *Element

	// The list to which this element belongs.
	list *List

	// A mutex protects all accesses to the list
	mutex sync.RWMutex

	// The value stored with this element.
	Value interface{}
}

// Next returns the next list element or nil.
func (e *Element) Next() *Element {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if p := e.next; e.list != nil && p != &e.list.tail {
		return p
	}
	return nil
}

// Prev returns the previous list element or nil.
func (e *Element) Prev() *Element {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if p := e.prev; e.list != nil && p != &e.list.head {
		return p
	}
	return nil
}

// List is a doubly linked list
// Implements the same interface as container.List
// Code heavily inspired by container.List
type List struct {
	// Separate sentinels avoid contention between operations at either end
	head, tail Element

	// Fixed size because of atomic access
	len int64
}

// init initializes list l.
// Does nothing it l alreahy initialised
func (l *List) lazyInit(clear bool) *List {
	initialised := false
	if l.Len() != 0 {
		initialised = true
	}

	if initialised && !clear {
		return l // Nothing to do, so avoid the locking operations
	}

	l.head.mutex.Lock()
	defer l.head.mutex.Unlock()
	l.tail.mutex.Lock()
	defer l.tail.mutex.Unlock()

	// double-checked locking
	if l.Len() != 0 {
		initialised = true
	}

	if !initialised || clear {
		atomic.StoreInt64(&l.len, 0)
		l.head.prev = nil
		l.head.list = l
		l.head.next = &l.tail
		l.tail.next = nil
		l.tail.list = l
		l.tail.prev = &l.head
	}
	return l
}

// Init initializes or clears list l.
func (l *List) Init() *List {
	return l.lazyInit(true)
}

// New returns an initialized list.
func New() *List {
	l := new(List)
	return l.lazyInit(false)
}

// Len returns the number of elements of list l.
// The complexity is O(1).
func (l *List) Len() int { return int(atomic.LoadInt64(&l.len)) }

// Front returns the first element of list l or nil if the list is empty.
func (l *List) Front() *Element {
	if l.Len() == 0 {
		return nil
	}

	l.head.mutex.RLock()
	defer l.head.mutex.RUnlock()
	// double-checked locking
	if l.Len() == 0 {
		return nil
	}
	return l.head.next
}

// Back returns the last element of list l or nil if the list is empty.
func (l *List) Back() *Element {
	if l.len == 0 {
		return nil
	}

	l.tail.mutex.RLock()
	defer l.tail.mutex.RUnlock()

	// double-checked locking
	if l.Len() == 0 {
		return nil
	}

	return l.tail.prev
}

// insertAfter inserts range [first, last] after at, increments l.len, and returns first.
// Elements in inserted range must not be accessed simultaneously.
func (l *List) insertAfter(first, last, at *Element) (*Element, bool) {
	nAdded := 1
	for e := first; e != last; e = e.next {
		e.list = l
		nAdded++
	}
	last.list = l

	at.mutex.Lock()
	defer at.mutex.Unlock()
	first.mutex.Lock()
	defer first.mutex.Unlock()
	if last != first {
		last.mutex.Lock()
		defer last.mutex.Unlock()
	}
	n := at.next
	if at.list != l || n == nil {
		// at is no longer in l, so we can't insert after it
		return nil, false
	}
	n.mutex.Lock()
	defer n.mutex.Unlock()

	at.next = first
	first.prev = at
	last.next = n
	n.prev = last
	atomic.AddInt64(&l.len, int64(nAdded))
	return first, true
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *List) insertValueAfter(v interface{}, at *Element) (*Element, bool) {
	e := &Element{Value: v}
	return l.insertAfter(e, e, at)
}

// Returns the predecessor of e in l in a thread safe way.
// The returned element, if not nil, is locked for writing.
func (l *List) predecessor(e *Element) *Element {
	e.mutex.RLock()
	p := e.prev
	for ; e.list == l && p != nil; p = e.prev {
		// We must unlock here to avoid deadlock: Always lock head-to-tail
		e.mutex.RUnlock()
		p.mutex.Lock()
		if p.next == e {
			return p
		}
		// We got a new predecessor before we got the lock, try again
		p.mutex.Unlock()
		e.mutex.RLock()
	}
	// If the loop terminates without returning, e was removed from l
	e.mutex.RUnlock()
	return nil
}

// insertBefore inserts range [first, last] before at, increments l.len.
// Returns the last inserted element, if any, and whether insertion was successful.
// Elements in inserted range must not be accessed simultaneously.
func (l *List) insertBefore(first, last, at *Element) (*Element, bool) {
	nAdded := 1
	for e := first; e != last; e = e.next {
		e.list = l
		nAdded++
	}
	last.list = l

	p := l.predecessor(at)
	if p == nil {
		// at is no longer in l, so we can't insert before it
		return nil, false
	}
	defer p.mutex.Unlock()
	first.mutex.Lock()
	defer first.mutex.Unlock()
	if last != first {
		last.mutex.Lock()
		defer last.mutex.Unlock()
	}
	at.mutex.Lock()
	defer at.mutex.Unlock()

	p.next = first
	first.prev = p
	last.next = at
	at.prev = last
	atomic.AddInt64(&l.len, int64(nAdded))
	return last, true
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *List) insertValueBefore(v interface{}, at *Element) (*Element, bool) {
	e := &Element{Value: v}
	return l.insertBefore(e, e, at)
}

// remove removes e from its list, decrements l.len. Returns e and whether this call removed it.
func (l *List) remove(e *Element) (*Element, bool) {
	p := l.predecessor(e)
	if p == nil {
		// Someone else already deleted e for us, we're done
		return e, false
	}
	defer p.mutex.Unlock()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	n := e.next
	n.mutex.Lock()
	defer n.mutex.Unlock()

	atomic.AddInt64(&l.len, -1)
	p.next = n
	n.prev = p
	e.next = nil // avoid memory leaks
	e.prev = nil // avoid memory leaks
	e.list = nil
	return e, true
}

// move moves e to next to at and returns e and whether move succeeded.
func (l *List) moveAfter(e, at *Element) (*Element, bool) {
	// Optimize away no-op moves
	if e == at {
		return e, true
	}
	at.mutex.RLock()
	if at.next == e {
		at.mutex.RUnlock()
		return e, true
	}
	if at.list != l {
		at.mutex.RUnlock()
		return e, false
	}
	at.mutex.RUnlock()
	// TODO: race condition if at is removed from l between here and inserting e
	// e will be removed from l and not inserted again

	_, ok := l.remove(e)
	if ok {
		_, ok = l.insertAfter(e, e, at)
	}
	return e, ok
}

// move moves e to next to at and returns e.
func (l *List) moveBefore(e, at *Element) (*Element, bool) {
	// Optimize away no-op moves
	if e == at {
		return e, true
	}
	at.mutex.RLock()
	if at.prev == e {
		at.mutex.RUnlock()
		return e, true
	}
	if at.list != l {
		at.mutex.RUnlock()
		return e, false
	}
	at.mutex.RUnlock()
	// TODO: race condition if at is removed from l between here and inserting e
	// e will be removed from l and not inserted again

	_, ok := l.remove(e)
	if ok {
		_, ok = l.insertBefore(e, e, at)
	}
	return e, ok
}

// Remove removes e from l if e is an element of list l.
// It returns the element value e.Value.
// The element must not be nil.
func (l *List) Remove(e *Element) interface{} {
	l.lazyInit(false)
	e, ok := l.remove(e)
	if ok {
		return e.Value
	}
	return nil
}

// PushFront inserts a new element e with value v at the front of list l and returns e.
func (l *List) PushFront(v interface{}) *Element {
	return l.InsertAfter(v, &l.head)
}

// PushBack inserts a new element e with value v at the back of list l and returns e.
func (l *List) PushBack(v interface{}) *Element {
	return l.InsertBefore(v, &l.tail)
}

// InsertBefore inserts a new element e with value v immediately before mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List) InsertBefore(v interface{}, mark *Element) *Element {
	// see comment in List.Remove about initialization of l
	l.lazyInit(false)
	e, _ := l.insertValueBefore(v, mark)
	return e
}

// InsertAfter inserts a new element e with value v immediately after mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List) InsertAfter(v interface{}, mark *Element) *Element {
	l.lazyInit(false)
	e, _ := l.insertValueAfter(v, mark)
	return e
}

// MoveToFront moves element e to the front of list l.
// If e is not an element of l, the list is not modified.
// The element must not be nil.
func (l *List) MoveToFront(e *Element) {
	if e.list != l {
		return
	}
	// see comment in List.Remove about initialization of l
	l.moveAfter(e, &l.head)
}

// MoveToBack moves element e to the back of list l.
// If e is not an element of l, the list is not modified.
// The element must not be nil.
func (l *List) MoveToBack(e *Element) {
	if e.list != l {
		return
	}
	// see comment in List.Remove about initialization of l
	l.moveBefore(e, &l.tail)
}

// MoveBefore moves element e to its new position before mark.
// If e or mark is not an element of l, or e == mark, the list is not modified.
// The element and mark must not be nil.
func (l *List) MoveBefore(e, mark *Element) {
	l.moveBefore(e, mark)
}

// MoveAfter moves element e to its new position after mark.
// If e or mark is not an element of l, or e == mark, the list is not modified.
// The element and mark must not be nil.
func (l *List) MoveAfter(e, mark *Element) {
	l.moveAfter(e, mark)
}

func (l *List) copyListElements() (*Element, *Element) {
	// TODO: Deal with modification of l during iteration
	tmp := New()
	for e := l.Front(); e != nil; e = e.Next() {
		tmp.insertValueBefore(e.Value, &tmp.tail)
	}
	return tmp.Front(), tmp.Back()
}

// PushBackList inserts a copy of an other list at the back of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List) PushBackList(other *List) {
	l.lazyInit(false)
	first, last := other.copyListElements()
	if first != nil && last != nil {
		l.insertBefore(first, last, &l.tail)
	}
}

// PushFrontList inserts a copy of an other list at the front of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List) PushFrontList(other *List) {
	l.lazyInit(false)
	first, last := other.copyListElements()
	if first != nil && last != nil {
		l.insertAfter(first, last, &l.head)
	}
}
