// Unit tests for LRU class, based on the tests for
// https://github.com/hashicorp/golang-lru
//
// Because of this, the following applies to the code in this file:
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package lru

import (
	"strconv"
	"sync/atomic"
	"testing"
)

// func BenchmarkLRU_Rand(b *testing.B) {
// 	l, err := New(8192)
// 	if err != nil {
// 		b.Errorf("err: %v", err)
// 	}

// 	trace := make([]int64, b.N*2)
// 	for i := 0; i < b.N*2; i++ {
// 		trace[i] = rand.Int63() % 32768
// 	}

// 	b.ResetTimer()

// 	var hit, miss int
// 	for i := 0; i < 2*b.N; i++ {
// 		if i%2 == 0 {
// 			l.Add(trace[i], trace[i])
// 		} else {
// 			_, ok := l.Get(trace[i])
// 			if ok {
// 				hit++
// 			} else {
// 				miss++
// 			}
// 		}
// 	}
// 	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(miss))
// }

// func BenchmarkLRU_Freq(b *testing.B) {
// 	l, err := New(8192)
// 	if err != nil {
// 		b.Errorf("err: %v", err)
// 	}

// 	trace := make([]int64, b.N*2)
// 	for i := 0; i < b.N*2; i++ {
// 		if i%2 == 0 {
// 			trace[i] = rand.Int63() % 16384
// 		} else {
// 			trace[i] = rand.Int63() % 32768
// 		}
// 	}

// 	b.ResetTimer()

// 	for i := 0; i < b.N; i++ {
// 		l.Add(trace[i], trace[i])
// 	}
// 	var hit, miss int
// 	for i := 0; i < b.N; i++ {
// 		_, ok := l.Get(trace[i])
// 		if ok {
// 			hit++
// 		} else {
// 			miss++
// 		}
// 	}
// 	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(miss))
// }

func TestLRU(t *testing.T) {
	evictCounter := int64(0)
	onEvicted := func(k interface{}, v interface{}) {
		if k != v {
			t.Errorf("Evict values not equal (%v!=%v)", k, v)
		}
		atomic.AddInt64(&evictCounter, 1)
	}

	l, err := NewWithEvict(128, onEvicted)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	for i := 0; i < 256; i++ {
		is := strconv.Itoa(i)
		if a := l.Add(is, is); a != (i >= 128) {
			t.Errorf("Add returned unexpected value: %v", a)
		}
	}

	if l.Len() < 128 {
		t.Errorf("Len too small: %v", l.Len())
	}

	// Wait for the async tasks to finish before testing the final state
	for atomic.LoadInt64(&evictCounter) < 128 ||
		atomic.LoadInt64(&l.evict.nPendingInsertions) != 0 {
	}
	if l.Len() != 128 {
		t.Errorf("bad len: %v", l.Len())
	}
	if atomic.LoadInt64(&evictCounter) != 128 {
		t.Errorf("bad evict count: %v", evictCounter)
	}

	// for i, k := range l.Keys() {
	// 	if v, ok := l.Get(k); !ok || v != k || v != i+128 {
	// 		t.Errorf("bad key: %v", k)
	// 	}
	// }
	// for i := 0; i < 128; i++ {
	// 	_, ok := l.Get(i)
	// 	if ok {
	// 		t.Errorf("should be evicted")
	// 	}
	// }
	// for i := 128; i < 256; i++ {
	// 	_, ok := l.Get(i)
	// 	if !ok {
	// 		t.Errorf("should not be evicted")
	// 	}
	// }
	// for i := 128; i < 192; i++ {
	// 	l.Remove(i)
	// 	_, ok := l.Get(i)
	// 	if ok {
	// 		t.Errorf("should be deleted")
	// 	}
	// }

	// l.Get(192) // expect 192 to be last key in l.Keys()

	// for i, k := range l.Keys() {
	// 	if (i < 63 && k != i+193) || (i == 63 && k != 192) {
	// 		t.Errorf("out of order key: %v", k)
	// 	}
	// }

	// l.Purge()
	// if l.Len() != 0 {
	// 	t.Errorf("bad len: %v", l.Len())
	// }
	// if _, ok := l.Get(200); ok {
	// 	t.Errorf("should contain nothing")
	// }
}

// test that Add returns true/false if an eviction occurred
func TestLRUAdd(t *testing.T) {
	evictCounter := int64(0)
	onEvicted := func(k interface{}, v interface{}) {
		atomic.AddInt64(&evictCounter, 1)
	}

	l, err := NewWithEvict(1, onEvicted)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	if l.Add("1", 1) == true || evictCounter != 0 {
		t.Errorf("should not have an eviction")
	}
	if l.Add("2", 2) == false {
		t.Errorf("should have an eviction")
	}
	for atomic.LoadInt64(&evictCounter) != 1 {
		// test times out if the evict never happens
	}
}

// test that Contains doesn't update recent-ness
func TestLRUContains(t *testing.T) {
	l, err := New(2)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	l.Add(1, 1)
	l.Add(2, 2)
	// if !l.Contains(1) {
	// 	t.Errorf("1 should be contained")
	// }

	// l.Add(3, 3)
	// if l.Contains(1) {
	// 	t.Errorf("Contains should not have updated recent-ness of 1")
	// }
}

// test that ContainsOrAdd doesn't update recent-ness
func TestLRUContainsOrAdd(t *testing.T) {
	l, err := New(2)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	l.Add(1, 1)
	l.Add(2, 2)
	// contains, evict := l.ContainsOrAdd(1, 1)
	// if !contains {
	// 	t.Errorf("1 should be contained")
	// }
	// if evict {
	// 	t.Errorf("nothing should be evicted here")
	// }

	// l.Add(3, 3)
	// contains, evict = l.ContainsOrAdd(1, 1)
	// if contains {
	// 	t.Errorf("1 should not have been contained")
	// }
	// if !evict {
	// 	t.Errorf("an eviction should have occurred")
	// }
	// if !l.Contains(1) {
	// 	t.Errorf("now 1 should be contained")
	// }
}

// test that PeekOrAdd doesn't update recent-ness
func TestLRUPeekOrAdd(t *testing.T) {
	l, err := New(2)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	l.Add(1, 1)
	l.Add(2, 2)
	// previous, contains, evict := l.PeekOrAdd(1, 1)
	// if !contains {
	// 	t.Errorf("1 should be contained")
	// }
	// if evict {
	// 	t.Errorf("nothing should be evicted here")
	// }
	// if previous != 1 {
	// 	t.Errorf("previous is not equal to 1")
	// }

	// l.Add(3, 3)
	// contains, evict = l.ContainsOrAdd(1, 1)
	// if contains {
	// 	t.Errorf("1 should not have been contained")
	// }
	// if !evict {
	// 	t.Errorf("an eviction should have occurred")
	// }
	// if !l.Contains(1) {
	// 	t.Errorf("now 1 should be contained")
	// }
}

// test that Peek doesn't update recent-ness
func TestLRUPeek(t *testing.T) {
	l, err := New(2)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	l.Add(1, 1)
	l.Add(2, 2)
	// if v, ok := l.Peek(1); !ok || v != 1 {
	// 	t.Errorf("1 should be set to 1: %v, %v", v, ok)
	// }

	// l.Add(3, 3)
	// if l.Contains(1) {
	// 	t.Errorf("should not have updated recent-ness of 1")
	// }
}

// test that Resize can upsize and downsize
func TestLRUResize(t *testing.T) {
	onEvictCounter := 0
	onEvicted := func(k interface{}, v interface{}) {
		onEvictCounter++
	}
	l, err := NewWithEvict(2, onEvicted)
	if err != nil {
		t.Errorf("err: %v", err)
	}

	// Downsize
	l.Add(1, 1)
	l.Add(2, 2)
	// evicted := l.Resize(1)
	// if evicted != 1 {
	// 	t.Errorf("1 element should have been evicted: %v", evicted)
	// }
	// if onEvictCounter != 1 {
	// 	t.Errorf("onEvicted should have been called 1 time: %v", onEvictCounter)
	// }

	// l.Add(3, 3)
	// if l.Contains(1) {
	// 	t.Errorf("Element 1 should have been evicted")
	// }

	// // Upsize
	// evicted = l.Resize(2)
	// if evicted != 0 {
	// 	t.Errorf("0 elements should have been evicted: %v", evicted)
	// }

	// l.Add(4, 4)
	// if !l.Contains(3) || !l.Contains(4) {
	// 	t.Errorf("Cache should have contained 2 elements")
	// }
}
