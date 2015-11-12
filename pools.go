// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sync

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// A Pools is a set of temporary objects that may be individually saved and
// retrieved.
//
// Any item stored in the Pools may be removed automatically at any time without
// notification. If the Pools holds the only reference when this happens, the
// item might be deallocated.
//
// A Pools is safe for use by multiple goroutines simultaneously.
//
// Pools's purpose is to cache allocated but unused items for later reuse,
// relieving pressure on the garbage collector. That is, it makes it easy to
// build efficient, thread-safe free lists. However, it is not suitable for all
// free lists.
//
// An appropriate use of a Pools is to manage a group of temporary items
// silently shared among and potentially reused by concurrent independent
// clients of a package. Pools provides a way to amortize allocation overhead
// across many clients.
//
// An example of good use of a Pools is in the fmt package, which maintains a
// dynamically-sized store of temporary output buffers. The store scales under
// load (when many goroutines are actively printing) and shrinks when
// quiescent.
//
// On the other hand, a free list maintained as part of a short-lived object is
// not a suitable use for a Pools, since the overhead does not amortize well in
// that scenario. It is more efficient to have such objects implement their own
// free list.
//

var (
	DefPoolsPrivateSize = 1024
)

type Pools struct {
	local     unsafe.Pointer // local fixed-size per-P pool, actual type is [P]poolsLocal
	localSize uintptr        // size of the local array

	// New optionally specifies a function to generate
	// a value when Get would otherwise return nil.
	// It may not be changed concurrently with calls to Get.
	PrivateSize int
	New         func() interface{}
}

// Local per-P Pools appendix.
type poolsLocal struct {
	private []interface{} // Can be used only by the respective P.
	shared  []interface{} // Can be used by any P.
	Mutex                 // Protects shared.
	pad     [128]byte     // Prevents false sharing.
}

// Put adds x to the pool.
func (p *Pools) Put(x interface{}) {
	if raceenabled {
		// Under race detector the Pools degenerates into no-op.
		// It's conforming, simple and does not introduce excessive
		// happens-before edges between unrelated goroutines.
		return
	}
	if x == nil {
		return
	}
	l := p.pin()
	if n := len(l.private) + 1; (p.PrivateSize > 0 && n <= p.PrivateSize) ||
		(p.PrivateSize == 0 && n <= DefPoolsPrivateSize) {
		l.private = append(l.private, x)
		x = nil
	}
	runtime_procUnpin()
	if x == nil {
		return
	}
	l.Lock()
	l.shared = append(l.shared, x)
	l.Unlock()
}

// Put adds xs to the pool.
func (p *Pools) Puts(xs []interface{}) {
	if raceenabled {
		// Under race detector the Pools degenerates into no-op.
		// It's conforming, simple and does not introduce excessive
		// happens-before edges between unrelated goroutines.
		return
	}
	xsl := len(xs)
	if xsl == 0 {
		return
	}
	l := p.pin()
	if n := len(l.private) + xsl; (p.PrivateSize > 0 && n <= p.PrivateSize) ||
		(p.PrivateSize == 0 && n <= DefPoolsPrivateSize) {
		l.private = append(l.private, xs...)
		xs = nil
	}
	runtime_procUnpin()
	if xs == nil {
		return
	}
	l.Lock()
	l.shared = append(l.shared, xs...)
	l.Unlock()
}

// Get selects an arbitrary item from the Pools, removes it from the
// Pools, and returns it to the caller.
// Get may choose to ignore the pool and treat it as empty.
// Callers should not assume any relation between values passed to Put and
// the values returned by Get.
//
// If Get would otherwise return nil and p.New is non-nil, Get returns
// the result of calling p.New.
func (p *Pools) Get() interface{} {
	if raceenabled {
		if p.New != nil {
			return p.New()
		}
		return nil
	}
	l := p.pin()
	var x interface{}
	if n := len(l.private); n > 0 {
		x = l.private[n-1]
		l.private = l.private[:n-1]
	}

	runtime_procUnpin()
	if x != nil {
		return x
	}
	l.Lock()
	last := len(l.shared) - 1
	if last >= 0 {
		x = l.shared[last]
		l.shared = l.shared[:last]
	}
	l.Unlock()
	if x != nil {
		return x
	}
	return p.getSlow()
}

func (p *Pools) getSlow() (x interface{}) {
	// See the comment in pin regarding ordering of the loads.
	size := atomic.LoadUintptr(&p.localSize) // load-acquire
	local := p.local                         // load-consume
	// Try to steal one element from other procs.
	pid := runtime_procPin()
	runtime_procUnpin()
	for i := 0; i < int(size); i++ {
		l := indexLocals(local, (pid+i+1)%int(size))
		l.Lock()
		last := len(l.shared) - 1
		if last >= 0 {
			x = l.shared[last]
			l.shared = l.shared[:last]
			l.Unlock()
			break
		}
		l.Unlock()
	}

	if x == nil && p.New != nil {
		x = p.New()
	}
	return x
}

// Get selects an arbitrary item from the Pools, removes it from the
// Pools, and returns it to the caller.
// Get may choose to ignore the pool and treat it as empty.
// Callers should not assume any relation between values passed to Put and
// the values returned by Get.
//
// If Get would otherwise return nil and p.New is non-nil, Get returns
// the result of calling p.New.
func (p *Pools) Gets(xs []interface{}) int {
	xsl := len(xs)
	if raceenabled {
		if p.New != nil {
			for i := 0; i < xsl; i++ {
				xs[i] = p.New()
			}
			return xsl
		}
		return 0
	}

	l := p.pin()
	gxs := xs[:0]
	if n := len(l.private); n >= xsl {
		gxs = append(gxs, l.private[n-xsl:]...)
		l.private = l.private[:n-xsl]
	} else if n > 0 {
		gxs = append(gxs, l.private...)
		l.private = l.private[:0]
	}

	runtime_procUnpin()
	gxsl := len(gxs)
	if gxsl == xsl {
		return xsl
	}
	l.Lock()

	if n, lack := len(l.shared), xsl-gxsl; n >= lack {
		gxs = append(gxs, l.shared[n-lack:]...)
		l.shared = l.shared[:n-lack]
	} else if n > 0 {
		gxs = append(gxs, l.shared...)
		l.shared = l.shared[:0]
	}
	l.Unlock()
	gxsl = len(gxs)
	if gxsl == xsl {
		return xsl
	}
	return p.getSlows(xs[gxsl:])
}

func (p *Pools) getSlows(xs []interface{}) int {
	xsl := len(xs)
	gxs := xs[:0]
	gxsl := 0
	// See the comment in pin regarding ordering of the loads.
	size := atomic.LoadUintptr(&p.localSize) // load-acquire
	local := p.local                         // load-consume
	// Try to steal one element from other procs.
	pid := runtime_procPin()
	runtime_procUnpin()
	for i := 0; i < int(size); i++ {
		l := indexLocals(local, (pid+i+1)%int(size))
		l.Lock()

		if n, lack := len(l.shared), xsl-len(gxs); n >= lack {
			gxs = append(gxs, l.shared[n-lack:]...)
			l.shared = l.shared[:n-lack]
		} else if n > 0 {
			gxs = append(gxs, l.shared...)
			l.shared = l.shared[:0]
		}

		gxsl = len(gxs)
		if gxsl == xsl {
			l.Unlock()
			break
		}
		l.Unlock()
	}

	if gxsl < xsl && p.New != nil {
		for i := gxsl; i < xsl; i++ {
			gxs = append(gxs, p.New())
		}
		return xsl
	} else {
		return gxsl
	}

}

// pin pins the current goroutine to P, disables preemption and returns poolsLocal pool for the P.
// Caller must call runtime_procUnpin() when done with the pool.
func (p *Pools) pin() *poolsLocal {
	pid := runtime_procPin()
	// In pinSlow we store to localSize and then to local, here we load in opposite order.
	// Since we've disabled preemption, GC can not happen in between.
	// Thus here we must observe local at least as large localSize.
	// We can observe a newer/larger local, it is fine (we must observe its zero-initialized-ness).
	s := atomic.LoadUintptr(&p.localSize) // load-acquire
	l := p.local                          // load-consume
	if uintptr(pid) < s {
		return indexLocals(l, pid)
	}
	return p.pinSlow()
}

func (p *Pools) pinSlow() *poolsLocal {
	// Retry under the mutex.
	// Can not lock the mutex while pinned.
	runtime_procUnpin()
	allPoolxsMu.Lock()
	defer allPoolxsMu.Unlock()
	pid := runtime_procPin()
	// poolsCleanup won't be called while we are pinned.
	s := p.localSize
	l := p.local
	if uintptr(pid) < s {
		return indexLocals(l, pid)
	}
	if p.local == nil {
		allPoolxs = append(allPoolxs, p)
	}
	// If GOMAXPROCS changes between GCs, we re-allocate the array and lose the old one.
	size := runtime.GOMAXPROCS(0)
	local := make([]poolsLocal, size)
	atomic.StorePointer((*unsafe.Pointer)(&p.local), unsafe.Pointer(&local[0])) // store-release
	atomic.StoreUintptr(&p.localSize, uintptr(size))                            // store-release
	return &local[pid]
}

func poolsCleanup() {
	// This function is called with the world stopped, at the beginning of a garbage collection.
	// It must not allocate and probably should not call any runtime functions.
	// Defensively zero out everything, 2 reasons:
	// 1. To prevent false retention of whole Pools.
	// 2. If GC happens while a goroutine works with l.shared in Put/Get,
	//    it will retain whole Pools. So next cycle memory consumption would be doubled.
	for i, p := range allPoolxs {
		allPoolxs[i] = nil
		for i := 0; i < int(p.localSize); i++ {
			l := indexLocals(p.local, i)
			l.private = nil
			for j := range l.shared {
				l.shared[j] = nil
			}
			l.shared = nil
		}
		p.local = nil
		p.localSize = 0
	}
	allPoolxs = []*Pools{}
}

var (
	allPoolxsMu Mutex
	allPoolxs   []*Pools
)

func init() {
	cleanup := func() {
		poolCleanup()
		poolsCleanup()
	}
	runtime_registerPoolCleanup(cleanup)
}

func indexLocals(l unsafe.Pointer, i int) *poolsLocal {
	return &(*[1000000]poolsLocal)(l)[i]
}

// Implemented in runtime.
//func runtime_registerPoolCleanup(cleanup func())
//func runtime_procPin() int
//func runtime_procUnpin()
