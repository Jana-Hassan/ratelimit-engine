package engine

import (
	"hash/fnv"
	"sync"
	"time"
)

type Node struct {
	prev         *Node
	next         *Node
	key          string
	freq         int
	value        interface{}
	lastAccessed time.Time
}

type frequencyList struct {
	head *Node
	tail *Node
	size int   // #nodes in the list
}

type shard struct{
	freqMap map[int]*frequencyList
	size   int
	capacity int
	minFreq int
	keyMap map[string]*Node
	mu sync.RWMutex
	
}

type Engine struct {
	shards [256]shard

}

func newFrequencyList() *frequencyList {
	tail := &Node{}
	head := &Node{
		next: tail,
	}
	tail.prev = head
	return &frequencyList{
		head: head,
		tail: tail,
		size: 0,
	}

}

func NewEngine(shardCapacity int) *Engine {
	if shardCapacity <= 0 {
		shardCapacity = 100
	}
	engine := &Engine{}
	for i := 0; i < 256; i++ {
		engine.shards[i] = shard{
			freqMap: make(map[int] *frequencyList),
			keyMap:  make(map[string] *Node),
			capacity: shardCapacity,
		}
	}
	return engine
}

func (freqlist *frequencyList) addToFront(newNode *Node) {
	newNode.prev = freqlist.head
	newNode.next = freqlist.head.next
	freqlist.head.next.prev = newNode
	freqlist.head.next = newNode
	freqlist.size++
}
func (freqlist *frequencyList) removeNode(node *Node) {
	node.prev.next = node.next
	node.next.prev = node.prev
	freqlist.size--
}

func (shard *shard) incrementFreq(node *Node) {
	currentFreq := node.freq  // remove the node from its current frequency list

	if freqList, exists := shard.freqMap[currentFreq]; 
	exists {
		freqList.removeNode(node)
		if freqList.size == 0 {
			delete(shard.freqMap, currentFreq)
			if shard.minFreq == currentFreq {
				shard.minFreq++
			}
		}
	}
	node.freq++
	if _, exists := shard.freqMap[node.freq]; 
	!exists {
		shard.freqMap[node.freq] = newFrequencyList()
	}
	shard.freqMap[node.freq].addToFront(node) 
}

func (engine *Engine) getShard(key string) *shard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	shardIndex := hash.Sum32() % 256
	return &engine.shards[shardIndex]
}

func (s *shard) evict() {
	list, exists := s.freqMap[s.minFreq]
	if !exists || list.size == 0 {
		return
	}
	lru := list.tail.prev
	list.removeNode(lru)
	if list.size == 0 {
		delete(s.freqMap, s.minFreq)
	}
	delete(s.keyMap, lru.key)
	s.size--
}

func (s *shard) set(key string, value interface{}) {
	if node, exists := s.keyMap[key]; exists {
		node.value = value
		s.incrementFreq(node)
		node.lastAccessed = time.Now()
		return
	}
	if s.size >= s.capacity {  // new key
		s.evict()
	}

	node := &Node{
		key:          key,
		value:        value,
		freq:         1,
		lastAccessed: time.Now(),
	}
	if _, exists := s.freqMap[1]; !exists {
		s.freqMap[1] = newFrequencyList()
	}
	s.freqMap[1].addToFront(node)
	s.keyMap[key] = node
	s.minFreq = 1
	s.size++
}

func (engine *Engine) Set(key string, value interface{}) {
	s := engine.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set(key, value)
}

func (engine *Engine) Delete(key string) {
	s := engine.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	node, exists := s.keyMap[key]
	if !exists {
		return
	}

	list := s.freqMap[node.freq]
	list.removeNode(node)
	if list.size == 0 {
		delete(s.freqMap, node.freq)
	}
	delete(s.keyMap, key)
	s.size--
}

func (s *shard) get(key string) (interface{}, bool) {
	if node, exists := s.keyMap[key]; exists {
		s.incrementFreq(node)
		node.lastAccessed = time.Now()
		return node.value, true
	}
	return nil, false
}

func (engine *Engine) Get(key string) (interface{}, bool) {
	s := engine.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(key)
}

func (engine *Engine) Update(key string, fn func(value interface{}, found bool) interface{}) {
	s := engine.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	var current interface{}
	node, found := s.keyMap[key]
	if found {
		current = node.value
	}
	s.set(key, fn(current, found))
}
