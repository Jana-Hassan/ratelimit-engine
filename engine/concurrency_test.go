package engine

import (
	"strconv"
	"sync"
	"testing"
)

func TestConcurrentMixedAccess(t *testing.T) {
	const (
		goroutines = 32
		iterations = 600
		keys       = 512
	)

	e := NewEngine(2)
	keyAt := func(i int) string { return "client:" + strconv.Itoa(i%keys) }

	var wg sync.WaitGroup
	start := make(chan struct{})

	worker := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				fn(i)
			}
		}()
	}

	for g := 0; g < goroutines; g++ {
		switch g % 6 {
		case 0:
			worker(func(i int) { e.Set(keyAt(i), i) })
		case 1:
			worker(func(i int) { e.Get(keyAt(i)) })
		case 2:
			worker(func(i int) {
				e.Update(keyAt(i), func(value interface{}, found bool) interface{} {
					if !found {
						return 1
					}
					return value.(int) + 1
				})
			})
		case 3:
			worker(func(i int) {
				e.View(keyAt(i), func(value interface{}, found bool) {
					if found {
						_ = value.(int)
					}
				})
			})
		case 4:
			worker(func(i int) { e.Delete(keyAt(i)) })
		case 5:
			worker(func(i int) { _ = e.Stats() })
		}
	}

	close(start)
	wg.Wait()

	for i := range e.shards {
		s := &e.shards[i]
		if s.size != len(s.keyMap) {
			t.Fatalf("shard %d: size is %d but keyMap holds %d", i, s.size, len(s.keyMap))
		}
		if s.size > s.capacity {
			t.Fatalf("shard %d: size %d exceeds capacity %d", i, s.size, s.capacity)
		}
		listed := 0
		for _, list := range s.freqMap {
			listed += list.size
		}
		if listed != s.size {
			t.Fatalf("shard %d: frequency lists hold %d nodes but size is %d", i, listed, s.size)
		}
	}
}
