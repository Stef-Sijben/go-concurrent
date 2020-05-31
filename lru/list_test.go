// Tests for a concurrent list used sequentially.
// Based on the unit tests for container.List, slightly adjusted and expanded.
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lru

import (
	"sync/atomic"
	"testing"
)

func checkListLen(t *testing.T, l *list, len int) bool {
	if n := l.Len(); n != len {
		t.Errorf("l.Len() = %d, want %d", n, len)
		return false
	}
	return true
}

// Wait for all async insertions to finish; this enforces serialisation
func (l *list) waitForInsertions() {
	lp := func() int64 {
		return atomic.LoadInt64(&l.nPendingInsertions)
	}
	for p := lp(); p > 0; p = lp() {
	}
}

func checkListPointers(t *testing.T, l *list, es []*element) {
	l.waitForInsertions()

	head := &l.head
	tail := &l.tail
	n := len(es)

	if !checkListLen(t, l, n) {
		return
	}

	if hl := head.list; hl != l {
		t.Errorf("head(%p).list = %p, want %p", head, hl, l)
	}
	if tl := tail.list; tl != l {
		t.Errorf("tail(%p).list = %p, want %p", head, tl, l)
	}

	// zero length lists must be properly initialized (head <--> tail cycle)
	if len(es) == 0 {
		if head.next != tail {
			t.Errorf("l.head.next = %p; should be %p", head.prev, tail)
		}
		if tail.prev != head {
			t.Errorf("l.tail.prev = %p; should be %p", tail.prev, head)
		}
		return
	}
	// len(es) > 0

	if hn := head.next; hn != es[0] {
		t.Errorf("head(%p).list = %p, want %p", head, hn, es[0])
	}
	if tp := tail.prev; tp != es[n-1] {
		t.Errorf("tail(%p).list = %p, want %p", head, tp, es[n-1])
	}

	// check internal and external prev/next connections
	for i, e := range es {
		prev := head
		if i > 0 {
			prev = es[i-1]
		}
		if p := e.prev; p != prev {
			t.Errorf("elt[%d](%p).prev = %p, want %p", i, e, p, prev)
		}

		next := tail
		if i < len(es)-1 {
			next = es[i+1]
		}
		if n := e.next; n != next {
			t.Errorf("elt[%d](%p).next = %p, want %p", i, e, n, next)
		}

		if el := e.list; el != l {
			t.Errorf("elt[%d](%p).list = %p, want %p", i, e, el, l)
		}
	}
}

func TestList(t *testing.T) {
	// Empty list
	l := newList()
	defer l.Close()
	checkListPointers(t, l, []*element{})

	// Single element list
	e := l.PushFront("a")
	checkListPointers(t, l, []*element{e})
	if !l.MoveToFront(e) {
		t.Error("MoveToFront returned false, expected true")
	}
	checkListPointers(t, l, []*element{e})
	if ep := l.PopBack(); ep != e {
		t.Errorf("PopBack returned %p, expected %p", ep, e)
	}
	checkListPointers(t, l, []*element{})

	// Bigger list
	e4 := l.PushFront("banana")
	e3 := l.PushFront(3)
	e2 := l.PushFront(2)
	e1 := l.PushFront(1)
	checkListPointers(t, l, []*element{e1, e2, e3, e4})

	if ep := l.PopBack(); ep != e4 {
		t.Errorf("PopBack returned %p, expected %p", ep, e4)
	}
	checkListPointers(t, l, []*element{e1, e2, e3})

	l.MoveToFront(e2) // move from middle
	checkListPointers(t, l, []*element{e2, e1, e3})

	l.MoveToFront(e3) // move from back
	checkListPointers(t, l, []*element{e3, e2, e1})
	l.MoveToFront(e3) // should be no-op
	checkListPointers(t, l, []*element{e3, e2, e1})

	e4 = l.PushFront(4) // insert before front
	checkListPointers(t, l, []*element{e4, e3, e2, e1})

	// Clear all elements
	if e := l.PopBack(); e != e1 {
		t.Errorf("PopBack returned %p, expected %p", e, e1)
	}
	checkListPointers(t, l, []*element{e4, e3, e2})
	if e := l.PopBack(); e != e2 {
		t.Errorf("PopBack returned %p, expected %p", e, e2)
	}
	checkListPointers(t, l, []*element{e4, e3})
	if e := l.PopBack(); e != e3 {
		t.Errorf("PopBack returned %p, expected %p", e, e3)
	}
	checkListPointers(t, l, []*element{e4})
	if e := l.PopBack(); e != e4 {
		t.Errorf("PopBack returned %p, expected %p", e, e4)
	}
	checkListPointers(t, l, []*element{})
}

func TestMoveBetweenLists(t *testing.T) {
	l1 := newList()
	defer l1.Close()
	e1 := l1.PushFront(1)
	e2 := l1.PushFront(2)
	e3 := l1.PushFront(3)
	e4 := l1.PushFront(4)
	checkListPointers(t, l1, []*element{e4, e3, e2, e1})

	l2 := newList()
	defer l2.Close()
	l2.MoveToFront(e2) // from middle
	checkListPointers(t, l1, []*element{e4, e3, e1})
	checkListPointers(t, l2, []*element{e2})

	l1.MoveToFront(e1) // within list
	checkListPointers(t, l1, []*element{e1, e4, e3})
	checkListPointers(t, l2, []*element{e2})

	l2.MoveToFront(e1) // from front
	checkListPointers(t, l1, []*element{e4, e3})
	checkListPointers(t, l2, []*element{e1, e2})

	l2.MoveToFront(e3) // from back
	checkListPointers(t, l1, []*element{e4})
	checkListPointers(t, l2, []*element{e3, e1, e2})

	l2.MoveToFront(e4) // only element
	checkListPointers(t, l1, []*element{})
	checkListPointers(t, l2, []*element{e4, e3, e1, e2})

	l1.MoveToFront(e1) // return to original list
	checkListPointers(t, l2, []*element{e4, e3, e2})
	checkListPointers(t, l1, []*element{e1})
}
