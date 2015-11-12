// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Pools is no-op under race detector, so all these tests do not work.
// +build !race

package sync_test

import (
	"runtime"
	"runtime/debug"
	. "sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPools(t *testing.T) {
	// disable GC so we can control when it happens.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	var p Pools
	if p.Get() != nil {
		t.Fatal("expected empty")
	}
	p.Put("a")
	p.Put("b")
	if g := p.Get(); g != "b" {
		t.Fatalf("got %#v; want a", g)
	}
	if g := p.Get(); g != "a" {
		t.Fatalf("got %#v; want a", g)
	}
	if g := p.Get(); g != nil {
		t.Fatalf("got %#v; want nil", g)
	}

	p.Puts([]interface{}{"a", "b"})
	if g := p.Get(); g != "b" {
		t.Fatalf("got %#v; want b", g)
	}
	if g := p.Get(); g != "a" {
		t.Fatalf("got %#v; want a", g)
	}
	if g := p.Get(); g != nil {
		t.Fatalf("got %#v; want nil", g)
	}

	p.Puts([]interface{}{"a", "b"})
	p.Puts([]interface{}{"c", "d"})
	if g := p.Get(); g != "d" {
		t.Fatalf("got %#v; want d", g)
	}
	if g := p.Get(); g != "c" {
		t.Fatalf("got %#v; want c", g)
	}
	if g := p.Get(); g != "b" {
		t.Fatalf("got %#v; want b", g)
	}
	if g := p.Get(); g != "a" {
		t.Fatalf("got %#v; want a", g)
	}
	if g := p.Get(); g != nil {
		t.Fatalf("got %#v; want nil", g)
	}

	putXs1 := []interface{}{"a", "b"}
	putXs2 := []interface{}{"c", "d"}
	getXs1 := []interface{}{nil, nil}
	getXs2 := []interface{}{nil, nil}
	p.Puts(putXs1)
	p.Puts(putXs2)
	if n := p.Gets(getXs2); n != 2 || getXs2[0] != "c" || getXs2[1] != "d" {
		t.Fatalf("got %#v; want %#+v", getXs2, putXs2)
	}
	if n := p.Gets(getXs1); n != 2 || getXs1[0] != "a" || getXs1[1] != "b" {
		t.Fatalf("got %#v; want %#+v", getXs1, putXs1)
	}
	getXs := []interface{}{nil, nil}
	if n := p.Gets(getXs); n != 0 || getXs[0] != nil || getXs[1] != nil {
		t.Fatalf("got %#v; want [nil,nil]", getXs)
	}

	p.Put("c")
	debug.SetGCPercent(100) // to allow following GC to actually run
	runtime.GC()
	if g := p.Get(); g != nil {
		t.Fatalf("got %#v; want nil after GC", g)
	}

	p.Puts(putXs1)
	p.Puts(putXs2)
	getXs = []interface{}{nil, nil}
	debug.SetGCPercent(100) // to allow following GC to actually run
	runtime.GC()
	if n := p.Gets(getXs); n != 0 || getXs[0] != nil || getXs[1] != nil {
		t.Fatalf("got %#v; want [nil,nil]", getXs)
	}
}

func TestPoolsPutGet(t *testing.T) {
	// disable GC so we can control when it happens.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	N := 10000 * 100
	var p = Pools{PrivateSize: N}
	for i := 0; i < N; i++ {
		p.Put(i)
	}
	for i := N - 1; i > 0; i-- {
		if n := p.Get(); n != i {
			t.Fatalf("got %v; want %d", n, i)
		}
	}
}
func TestPoolsNew(t *testing.T) {
	// disable GC so we can control when it happens.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	i := 0
	p := Pools{
		New: func() interface{} {
			i++
			return i
		},
	}
	if v := p.Get(); v != 1 {
		t.Fatalf("got %v; want 1", v)
	}
	if v := p.Get(); v != 2 {
		t.Fatalf("got %v; want 2", v)
	}
	p.Put(42)
	if v := p.Get(); v != 42 {
		t.Fatalf("got %v; want 42", v)
	}
	if v := p.Get(); v != 3 {
		t.Fatalf("got %v; want 3", v)
	}
}

// Test that Pools does not hold pointers to previously cached resources.
func TestPoolsGC(t *testing.T) {
	testPools(t, true)
}

// Test that Pools releases resources on GC.
func TestPoolsRelease(t *testing.T) {
	testPools(t, false)
}

func testPools(t *testing.T, drain bool) {
	var p Pools
	const N = 100
loop:
	for try := 0; try < 3; try++ {
		var fin, fin1 uint32
		for i := 0; i < N; i++ {
			v := new(string)
			runtime.SetFinalizer(v, func(vv *string) {
				atomic.AddUint32(&fin, 1)
			})
			p.Put(v)
		}
		if drain {
			for i := 0; i < N; i++ {
				p.Get()
			}
		}
		for i := 0; i < 5; i++ {
			runtime.GC()
			time.Sleep(time.Duration(i*100+10) * time.Millisecond)
			// 1 pointer can remain on stack or elsewhere
			if fin1 = atomic.LoadUint32(&fin); fin1 >= N-1 {
				continue loop
			}
		}
		t.Fatalf("only %v out of %v resources are finalized on try %v", fin1, N, try)
	}
}

func TestPoolsStress(t *testing.T) {
	const P = 10
	N := int(1e6)
	if testing.Short() {
		N /= 100
	}
	var p Pools
	done := make(chan bool)
	for i := 0; i < P; i++ {
		go func() {
			var v interface{} = 0
			for j := 0; j < N; j++ {
				if v == nil {
					v = 0
				}
				p.Put(v)
				v = p.Get()
				if v != nil && v.(int) != 0 {
					t.Fatalf("expect 0, got %v", v)
				}
			}
			done <- true
		}()
	}
	for i := 0; i < P; i++ {
		<-done
	}
}

func enchmarkPoolsPutGet(b *testing.B) {
	var p Pools
	var v = 1
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Put(&v)
			p.Get()
		}
	})
}

func benchmarkPoolsPutGets(b *testing.B, n int) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	b.StartTimer()
	var p Pools
	var v = 1
	p.New = func() interface{} { return &v }
	putXs := []interface{}{&v}
	getXs := []interface{}{nil}
	for i := 0; i < n; i++ {
		putXs = append(putXs, putXs...)
		getXs = append(getXs, getXs...)
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Puts(putXs)
			n := p.Gets(getXs)
			if n != len(getXs) {
				b.Errorf("expect len %d, got %v", len(getXs), n)
			}
		}
	})
	b.StopTimer()
}

func BenchmarkPoolsPutGets____1(b *testing.B) {
	benchmarkPoolsPutGets(b, 0)
}

func BenchmarkPoolsPutGets____2(b *testing.B) {
	benchmarkPoolsPutGets(b, 1)
}

func BenchmarkPoolsPutGets____4(b *testing.B) {
	benchmarkPoolsPutGets(b, 2)
}

func BenchmarkPoolsPutGets____8(b *testing.B) {
	benchmarkPoolsPutGets(b, 3)
}

func BenchmarkPoolsPutGets___16(b *testing.B) {
	benchmarkPoolsPutGets(b, 4)
}

func BenchmarkPoolsPutGets___32(b *testing.B) {
	benchmarkPoolsPutGets(b, 5)
}

func BenchmarkPoolsPutGets___64(b *testing.B) {
	benchmarkPoolsPutGets(b, 6)
}

func BenchmarkPoolsPutGets__128(b *testing.B) {
	benchmarkPoolsPutGets(b, 7)
}

func BenchmarkPoolsPutGets__256(b *testing.B) {
	benchmarkPoolsPutGets(b, 8)
}

func BenchmarkPoolsPutGets__512(b *testing.B) {
	benchmarkPoolsPutGets(b, 9)
}

func BenchmarkPoolsPutGets_1024(b *testing.B) {
	benchmarkPoolsPutGets(b, 10)
}

func BenchmarkPoolsOverflow(b *testing.B) {
	var p Pool
	var v = 1
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for b := 0; b < 100; b++ {
				p.Put(&v)
			}
			for b := 0; b < 100; b++ {
				p.Get()
			}
		}
	})
}

func benchmarkPoolsOverflows(b *testing.B, n int) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	b.StartTimer()
	var p Pools
	var v = 1
	p.New = func() interface{} { return &v }
	putXs := []interface{}{&v}
	getXs := []interface{}{nil}
	for i := 0; i < n; i++ {
		putXs = append(putXs, putXs...)
		getXs = append(getXs, getXs...)
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for i := 0; i < 100; i++ {
				p.Puts(putXs)
			}
			for i := 0; i < 100; i++ {
				n := p.Gets(getXs)
				if n != len(getXs) {
					b.Errorf("expect len %d, got %v", len(getXs), n)
				}
			}
		}
	})
	b.StopTimer()
	b.N = b.N * 100
}

func BenchmarkPoolsOverflows____1(b *testing.B) {
	benchmarkPoolsOverflows(b, 0)
}

func BenchmarkPoolsOverflows____2(b *testing.B) {
	benchmarkPoolsOverflows(b, 1)
}

func BenchmarkPoolsOverflows____4(b *testing.B) {
	benchmarkPoolsOverflows(b, 2)
}

func BenchmarkPoolsOverflows____8(b *testing.B) {
	benchmarkPoolsOverflows(b, 3)
}

func BenchmarkPoolsOverflows___16(b *testing.B) {
	benchmarkPoolsOverflows(b, 4)
}

func BenchmarkPoolsOverflows___32(b *testing.B) {
	benchmarkPoolsOverflows(b, 5)
}

func BenchmarkPoolsOverflows___64(b *testing.B) {
	benchmarkPoolsOverflows(b, 6)
}

func BenchmarkPoolsOverflows__128(b *testing.B) {
	benchmarkPoolsOverflows(b, 7)
}

func BenchmarkPoolsOverflows__256(b *testing.B) {
	benchmarkPoolsOverflows(b, 8)
}

func BenchmarkPoolsOverflows__512(b *testing.B) {
	benchmarkPoolsOverflows(b, 9)
}

func BenchmarkPoolsOverflows_1024(b *testing.B) {
	benchmarkPoolsOverflows(b, 9)
}
