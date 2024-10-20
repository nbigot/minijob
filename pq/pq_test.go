package pq

import (
	"testing"
)

// Equal function for integers
func intEqual(a, b int) bool {
	return a == b
}

// TestEnqueue tests the Enqueue method of the PriorityQueue
func TestEnqueue(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	pq.Enqueue(10, 1)
	pq.Enqueue(20, 2)
	pq.Enqueue(15, 1)
	pq.Enqueue(4, 0)

	if pq.length != 4 {
		t.Errorf("Expected length 4, got %d", pq.length)
	}

	if pq.head.value != 20 {
		t.Errorf("Expected head value 20, got %d", pq.head.value)
	}

	if pq.head.next.value != 10 {
		t.Errorf("Expected second value 10, got %d", pq.head.next.value)
	}

	if pq.head.next.next.value != 15 {
		t.Errorf("Expected third value 15, got %d", pq.head.next.next.value)
	}

	if pq.head.next.next.next.value != 4 {
		t.Errorf("Expected fourth value 4, got %d", pq.head.next.next.next.value)
	}
}

// TestPeek tests the Peek method of the PriorityQueue
func TestPeek(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	_, priority, err := pq.Peek()
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if pq.Len() != 0 {
		t.Errorf("Expected length 0, got %d", pq.Len())
	}
	if priority != 0 {
		t.Errorf("Expected priority 0, got %d", priority)
	}

	pq.Enqueue(10, 1)
	value, priority, err := pq.Peek()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if value != 10 {
		t.Errorf("Expected value 10, got %d", value)
	}

	if pq.Len() != 1 {
		t.Errorf("Expected length 1, got %d", pq.Len())
	}
	if priority != 1 {
		t.Errorf("Expected priority 1, got %d", priority)
	}

	pq.Enqueue(20, 2)
	value, priority, err = pq.Peek()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if value != 20 {
		t.Errorf("Expected value 20, got %d", value)
	}

	if pq.Len() != 2 {
		t.Errorf("Expected length 2, got %d", pq.Len())
	}
	if priority != 2 {
		t.Errorf("Expected priority 2, got %d", priority)
	}
}

// TestDequeue tests the Dequeue method of the PriorityQueue
func TestDequeue(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	_, priority, err := pq.Dequeue()
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if priority != 0 {
		t.Errorf("Expected priority 0, got %d", priority)
	}

	pq.Enqueue(10, 1)
	pq.Enqueue(20, 2)
	value, priority, err := pq.Dequeue()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if value != 20 {
		t.Errorf("Expected value 20, got %d", value)
	}
	if priority != 2 {
		t.Errorf("Expected priority 2, got %d", priority)
	}

	value, priority, err = pq.Dequeue()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if value != 10 {
		t.Errorf("Expected value 10, got %d", value)
	}
	if priority != 1 {
		t.Errorf("Expected priority 1, got %d", priority)
	}

	_, priority, err = pq.Dequeue()
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if priority != 0 {
		t.Errorf("Expected priority 0, got %d", priority)
	}
}

// TestDequeueWithPriority tests the DequeueWithPriority method of the PriorityQueue
func TestDequeueWithPriority(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	_, priority, ok := pq.DequeueWithPriority(1)
	if ok {
		t.Errorf("Expected ok to be false, got true")
	}
	if priority != 0 {
		t.Errorf("Expected priority 0, got %d", priority)
	}

	pq.Enqueue(10, 1)
	pq.Enqueue(20, 2)
	value, priority, ok := pq.DequeueWithPriority(2)
	if !ok {
		t.Errorf("Expected ok to be true, got false")
	}
	if value != 20 {
		t.Errorf("Expected value 20, got %d", value)
	}
	if priority != 2 {
		t.Errorf("Expected priority 2, got %d", priority)
	}

	value, priority, ok = pq.DequeueWithPriority(1)
	if !ok {
		t.Errorf("Expected ok to be true, got false")
	}
	if value != 10 {
		t.Errorf("Expected value 10, got %d", value)
	}
	if priority != 1 {
		t.Errorf("Expected priority 1, got %d", priority)
	}

	_, priority, ok = pq.DequeueWithPriority(1)
	if ok {
		t.Errorf("Expected ok to be false, got true")
	}
	if priority != 0 {
		t.Errorf("Expected priority 0, got %d", priority)
	}
}

// TestLen tests the Len method of the PriorityQueue
func TestLen(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	if pq.Len() != 0 {
		t.Errorf("Expected length 0, got %d", pq.Len())
	}

	pq.Enqueue(10, 1)
	if pq.Len() != 1 {
		t.Errorf("Expected length 1, got %d", pq.Len())
	}

	pq.Enqueue(20, 2)
	if pq.Len() != 2 {
		t.Errorf("Expected length 2, got %d", pq.Len())
	}

	pq.Dequeue()
	if pq.Len() != 1 {
		t.Errorf("Expected length 1, got %d", pq.Len())
	}
}

// TestRemove tests the Remove method of the PriorityQueue
func TestRemove(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	err := pq.Remove(10)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}

	pq.Enqueue(10, 1)
	pq.Enqueue(20, 2)
	err = pq.Remove(10)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if pq.Len() != 1 {
		t.Errorf("Expected length 1, got %d", pq.Len())
	}

	if pq.head.value != 20 {
		t.Errorf("Expected head value 20, got %d", pq.head.value)
	}

	err = pq.Remove(20)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if pq.Len() != 0 {
		t.Errorf("Expected length 0, got %d", pq.Len())
	}

	if pq.head != nil {
		t.Errorf("Expected head to be nil, got %v", pq.head)
	}

	pq.Enqueue(10, 0)
	pq.Enqueue(20, 0)
	pq.Enqueue(30, 0)
	pq.Enqueue(40, 0)

	err = pq.Remove(20)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if pq.Len() != 3 {
		t.Errorf("Expected length 3, got %d", pq.Len())
	}

	if pq.head.value != 10 {
		t.Errorf("Expected head value 10, got %d", pq.head.value)
	}

	if pq.head.next.value != 30 {
		t.Errorf("Expected second value 30, got %d", pq.head.next.value)
	}

	if pq.head.next.next.value != 40 {
		t.Errorf("Expected third value 40, got %d", pq.head.next.next.value)
	}
}

// TestIsEmpty tests the IsEmpty method of the PriorityQueue
func TestIsEmpty(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	if !pq.IsEmpty() {
		t.Errorf("Expected true, got false")
	}

	pq.Enqueue(10, 1)
	if pq.IsEmpty() {
		t.Errorf("Expected false, got true")
	}

	pq.Dequeue()
	if !pq.IsEmpty() {
		t.Errorf("Expected true, got false")
	}
}

// TestClear tests the Clear method of the PriorityQueue
func TestClear(t *testing.T) {
	pq := NewPriorityQueue(intEqual)
	pq.Enqueue(10, 1)
	pq.Enqueue(20, 2)
	pq.Enqueue(15, 1)

	pq.Clear()

	if pq.Len() != 0 {
		t.Errorf("Expected length 0 after clear, got %d", pq.Len())
	}

	if !pq.IsEmpty() {
		t.Errorf("Expected true for IsEmpty after clear, got false")
	}

	if pq.head != nil {
		t.Errorf("Expected head to be nil after clear, got %v", pq.head)
	}
}
