package concurrent

import (
	"sync"
	"sync/atomic"
)

// Element is an element of a linked list.
type Element struct {
	// Next and previous pointers in the doubly-linked list of elements.
	// To simplify the implementation, internally a list l is implemented
	// as a ring, such that &l.root is both the next element of the last
	// list element (l.Back()) and the previous element of the first list
	// element (l.Front()).
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
	// Initialise these only at creation, they should never change
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

// insert inserts e after at, increments l.len, and returns e.
func (l *List) insertAfter(e, at *Element) *Element {
	at.mutex.Lock()
	defer at.mutex.Unlock()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	n := at.next
	n.mutex.Lock()
	defer n.mutex.Unlock()

	at.next = e
	e.prev = at
	e.next = n
	n.prev = e
	e.list = l
	atomic.AddInt64(&l.len, 1)
	return e
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *List) insertValueAfter(v interface{}, at *Element) *Element {
	return l.insertAfter(&Element{Value: v}, at)
}

// insert inserts e before at, increments l.len, and returns e.
func (l *List) insertBefore(e, at *Element) *Element {
	p := at.prev
	for ; ; p = at.prev {
		p.mutex.Lock()
		if p.next == at {
			defer p.mutex.Unlock()
			break
		}
		// at got a new predecessor before we got the lock, try again
		p.mutex.Unlock()
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	at.mutex.Lock()
	defer at.mutex.Unlock()

	p.next = e
	e.prev = p
	e.next = at
	at.prev = e
	e.list = l
	atomic.AddInt64(&l.len, 1)
	return e
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *List) insertValueBefore(v interface{}, at *Element) *Element {
	return l.insertBefore(&Element{Value: v}, at)
}

// remove removes e from its list, decrements l.len, and returns e.
func (l *List) remove(e *Element) *Element {
	p := e.prev
	for ; ; p = e.prev {
		p.mutex.Lock()
		if p.next == e {
			defer p.mutex.Unlock()
			break
		}
		// We got a new predecessor before we got the lock, try again
		p.mutex.Unlock()
	}
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
	return e
}

// move moves e to next to at and returns e.
func (l *List) moveAfter(e, at *Element) *Element {
	// TODO: optimize away no-op moves?
	if e == at {
		return e
	}

	l.remove(e)
	l.insertAfter(e, at)
	return e
}

// move moves e to next to at and returns e.
func (l *List) moveBefore(e, at *Element) *Element {
	// TODO: optimize away no-op moves?
	if e == at {
		return e
	}

	l.remove(e)
	l.insertBefore(e, at)
	return e
}

// Remove removes e from l if e is an element of list l.
// It returns the element value e.Value.
// The element must not be nil.
func (l *List) Remove(e *Element) interface{} {
	if e.list == l {
		// if e.list == l, l must have been initialized when e was inserted
		// in l or l == nil (e is a zero Element) and l.remove will crash
		l.remove(e)
	}
	return e.Value
}

// PushFront inserts a new element e with value v at the front of list l and returns e.
func (l *List) PushFront(v interface{}) *Element {
	l.lazyInit(false)
	return l.insertValueAfter(v, &l.head)
}

// PushBack inserts a new element e with value v at the back of list l and returns e.
func (l *List) PushBack(v interface{}) *Element {
	l.lazyInit(false)
	return l.insertValueBefore(v, &l.tail)
}

// InsertBefore inserts a new element e with value v immediately before mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List) InsertBefore(v interface{}, mark *Element) *Element {
	if mark.list != l {
		return nil
	}
	// see comment in List.Remove about initialization of l
	return l.insertValueBefore(v, mark)
}

// InsertAfter inserts a new element e with value v immediately after mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List) InsertAfter(v interface{}, mark *Element) *Element {
	if mark.list != l {
		return nil
	}
	// see comment in List.Remove about initialization of l
	return l.insertValueAfter(v, mark)
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
	if e.list != l || e == mark || mark.list != l {
		return
	}
	l.moveBefore(e, mark)
}

// MoveAfter moves element e to its new position after mark.
// If e or mark is not an element of l, or e == mark, the list is not modified.
// The element and mark must not be nil.
func (l *List) MoveAfter(e, mark *Element) {
	if e.list != l || e == mark || mark.list != l {
		return
	}
	l.moveAfter(e, mark)
}

// PushBackList inserts a copy of an other list at the back of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List) PushBackList(other *List) {
	// TODO: Make atomic
	l.lazyInit(false)
	for i, e := other.Len(), other.Front(); i > 0; i, e = i-1, e.Next() {
		l.insertValueBefore(e.Value, &l.tail)
	}
}

// PushFrontList inserts a copy of an other list at the front of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List) PushFrontList(other *List) {
	// TODO: Make atomic
	l.lazyInit(false)
	for i, e := other.Len(), other.Back(); i > 0; i, e = i-1, e.Prev() {
		l.insertValueAfter(e.Value, &l.head)
	}
}
