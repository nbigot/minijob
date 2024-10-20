package pq

import (
	"fmt"
)

// IPriotityQueue is the interface for a priority queue
type IPriorityQueue[T any] interface {
	// Enqueue adds a new value to the priority queue
	Enqueue(value T, priority int64)
	// Peek returns the value at the front of the queue without removing it (with the highest priority)
	Peek() (T, int64, error)
	// Dequeue removes and returns the value at the front of the queue
	Dequeue() (T, int64, error)
	// Dequeue removes and returns the value at the front of the queue with priority >= minPriority
	DequeueWithPriority(minPriority int64) (T, int64, bool)
	// Len returns the number of elements in the queue
	Len() int
	// Remove find and removes a specific value from the queue
	Remove(value T) error
	// IsEmpty returns true if the queue is empty
	IsEmpty() bool
	// Clear removes all elements from the queue
	Clear()
}

// Node represents a node in the singly linked list
type Node[T any] struct {
	value    T
	priority int64
	next     *Node[T]
}

// PriorityQueue implements the IPriotityQueue interface using a singly linked list
type PriorityQueue[T any] struct {
	head   *Node[T]
	length int
	equal  func(a, b T) bool
}

// Enqueue adds a new value to the priority queue in sorted order
func (pq *PriorityQueue[T]) Enqueue(value T, priority int64) {
	newNode := &Node[T]{value: value, priority: priority}
	if pq.head == nil || pq.head.priority < priority {
		newNode.next = pq.head
		pq.head = newNode
	} else {
		current := pq.head
		for current.next != nil && current.next.priority >= priority {
			current = current.next
		}
		newNode.next = current.next
		current.next = newNode
	}
	pq.length++
}

// Peek returns the value at the front of the queue without removing it
func (pq *PriorityQueue[T]) Peek() (T, int64, error) {
	if pq.head == nil {
		var zero T
		return zero, 0, fmt.Errorf("queue is empty")
	}
	return pq.head.value, pq.head.priority, nil
}

// Dequeue removes and returns the value at the front of the queue
func (pq *PriorityQueue[T]) Dequeue() (T, int64, error) {
	if pq.head == nil {
		var zero T
		return zero, 0, fmt.Errorf("queue is empty")
	}
	value := pq.head.value
	priority := pq.head.priority
	pq.head = pq.head.next
	pq.length--
	return value, priority, nil
}

// Dequeue removes and returns the value at the front of the queue with priority >= minPriority
func (pq *PriorityQueue[T]) DequeueWithPriority(minPriority int64) (T, int64, bool) {
	if pq.head == nil || pq.head.priority < minPriority {
		var zero T
		return zero, 0, false
	}
	value := pq.head.value
	priority := pq.head.priority
	pq.head = pq.head.next
	pq.length--
	return value, priority, true
}

// Len returns the number of elements in the queue
func (pq *PriorityQueue[T]) Len() int {
	return pq.length
}

// Remove find and removes a specific value from the queue
func (pq *PriorityQueue[T]) Remove(value T) error {
	if pq.head == nil {
		return fmt.Errorf("queue is empty")
	}

	if pq.equal(pq.head.value, value) {
		pq.head = pq.head.next
		pq.length--
		return nil
	}

	current := pq.head
	for current.next != nil {
		if pq.equal(current.next.value, value) {
			current.next = current.next.next
			pq.length--
			return nil
		}
		current = current.next
	}

	return fmt.Errorf("value not found in the queue")
}

// IsEmpty returns true if the queue is empty
func (pq *PriorityQueue[T]) IsEmpty() bool {
	return pq.head == nil
}

// Clear removes all elements from the queue
func (pq *PriorityQueue[T]) Clear() {
	pq.head = nil
	pq.length = 0
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue[T any](equal func(a, b T) bool) *PriorityQueue[T] {
	return &PriorityQueue[T]{equal: equal}
}
