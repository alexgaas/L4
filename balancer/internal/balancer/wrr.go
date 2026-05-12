package balancer

import (
	"balancer/internal/io"
	"container/heap"
	"fmt"
)

// Here goes EDF implementation https://en.wikipedia.org/wiki/Earliest_deadline_first_scheduling
// use a priority queue because Go has built-in heap container

type WrrBalancer struct {
	nodes         wrrPriorityQueue
	entryPounters []*wrrQueueEntry
}

func NewWrrBalancer() *WrrBalancer {
	return &WrrBalancer{entryPounters: make([]*wrrQueueEntry, 0)}
}

func (t *WrrBalancer) Check(weight float64) error {
	// TODO: replace with smth like numeric_limits epsilon
	if weight == 0.0 {
		return fmt.Errorf("incorrect backend weight, should not be zero")
	}

	return nil
}

func (t *WrrBalancer) Update(idx int, weight float64) error {
	if err := t.Check(weight); err != nil {
		return err
	}

	if idx > len(t.entryPounters) {
		return fmt.Errorf("index of item is greater than queue length")
	}

	w := 1.0 / weight

	// Update only WeightStep to save previous state
	t.entryPounters[idx].WeightStep = w

	// rebalance queue
	heap.Fix(&t.nodes, 0)

	return nil
}

func (t *WrrBalancer) Add(idx int, weight float64, _ []byte) error {
	if err := t.Check(weight); err != nil {
		return err
	}

	w := 1.0 / weight
	entry := wrrQueueEntry{
		Weight:     w,
		WeightStep: w,
		BackendIdx: idx,
	}

	t.entryPounters = append(t.entryPounters, &entry)

	heap.Push(&t.nodes, &entry)

	return nil
}

func (t *WrrBalancer) next() int {
	if len(t.nodes) == 0 {
		return -1
	}

	next := t.nodes[0]
	next.IncSelfUsageCount()
	// rebalance queue
	heap.Fix(&t.nodes, 0)

	return next.BackendIdx
}

func (t *WrrBalancer) FillData(messages []io.RecvMmsgData, cnt int, balancer *Balancer) {
	for i := 0; i < cnt; i++ {
		backendIdx := -1

		for j := 0; j < balancer.MaxRetry; j++ {
			wrrIdx := t.next()

			if wrrIdx < 0 {
				Log.Error("Incorrect balancer configuration")
				return
			}

			if !balancer.Backends[wrrIdx].Alive.Load() {
				continue
			}

			backendIdx = wrrIdx
			break
		}

		if backendIdx < 0 {
			Log.Error("Could not select backend")
			continue
		}

		msgIdx := balancer.QueueByBackends[backendIdx].EnqCount

		balancer.QueueByBackends[backendIdx].Data[msgIdx] = messages[i]
		balancer.QueueByBackends[backendIdx].EnqCount++
	}
}

// Priority queue based on examples from https://golang.org/pkg/container/heap/

type wrrQueueEntry struct {
	Weight     float64
	WeightStep float64
	BackendIdx int
}

func (t *wrrQueueEntry) IncSelfUsageCount() {
	t.Weight += t.WeightStep
}

type wrrPriorityQueue []*wrrQueueEntry

// Basic operations for pq

func (pq wrrPriorityQueue) Len() int {
	return len(pq)
}

func (pq wrrPriorityQueue) Less(i, j int) bool {
	return pq[i].Weight < pq[j].Weight || (pq[i].Weight == pq[j].Weight && pq[i].WeightStep < pq[j].WeightStep)
}

func (pq wrrPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *wrrPriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*wrrQueueEntry))
}

func (pq *wrrPriorityQueue) Pop() interface{} {
	old := *pq
	*pq = old[0 : len(old)-1]
	return old[len(old)-1]
}
