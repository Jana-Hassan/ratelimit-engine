package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSetAndGet(t *testing.T) {
	e := NewEngine(10)
	e.Set("user:ahmed", 42)

	val, ok := e.Get("user:ahmed")
	if !ok {
		t.Fatalf("expected key 'user:ahmed' to exist, got miss")
	}
	if val != 42 {
		t.Errorf("expected value 42, got %v", val)
	}
}

func TestEviction(t *testing.T) {
	s := &shard{
		freqMap:  make(map[int]*frequencyList),
		keyMap:   make(map[string]*Node),
		capacity: 2,
	}

	nodeA := &Node{key: "a", value: 1, freq: 1, lastAccessed: time.Now()}
	s.freqMap[1] = newFrequencyList()
	s.freqMap[1].addToFront(nodeA)
	s.keyMap["a"] = nodeA
	s.size++

	nodeB := &Node{key: "b", value: 2, freq: 1, lastAccessed: time.Now()}
	s.freqMap[1].addToFront(nodeB)
	s.keyMap["b"] = nodeB
	s.size++

	s.minFreq = 1

	s.incrementFreq(nodeA) // a → freq 2, b stays at freq 1

	s.evict() // should evict b

	if _, exists := s.keyMap["b"]; exists {
		t.Errorf("expected 'b' to be evicted")
	}
	if _, exists := s.keyMap["a"]; !exists {
		t.Errorf("expected 'a' to survive")
	}
}

func TestDelete(t *testing.T) {
	e := NewEngine(10)
	e.Set("x", 99)
	e.Delete("x")

	if _, ok := e.Get("x"); ok {
		t.Errorf("expected 'x' to be deleted, but it still exists")
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := NewEngine(1000)
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("user:%d", i)
			e.Set(key, i)
			e.Get(key)
		}(i)
	}
	wg.Wait()
}
