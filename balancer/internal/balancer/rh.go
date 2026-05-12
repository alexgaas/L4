package balancer

import (
	"balancer/internal/io"
	"bytes"
	"hash/fnv"
	"math"
	"syscall"
	"unsafe"
)

const fiftyThreeOnes = uint64(0xFFFFFFFFFFFFFFFF >> (64 - 53))
const fiftyThreeZeros = float64(1 << 53)

type RendezvousBalancer struct {
	nodes []RendezvousEntry
}

type RendezvousEntry struct {
	Idx    int
	Weight float64
	Seed   []byte
}

func NewRendezvousBalancer() *RendezvousBalancer {
	return &RendezvousBalancer{}
}

func (t *RendezvousBalancer) Add(idx int, weight float64, seed []byte) error {
	t.nodes = append(t.nodes, RendezvousEntry{
		Idx:    idx,
		Weight: weight,
		Seed:   seed,
	})

	return nil
}

func (t *RendezvousBalancer) Update(idx int, weight float64) error {
	// There is no any advanced structures
	// Just update value by index
	t.nodes[idx].Weight = weight
	return nil
}

func intToFloat(v uint64) float64 {
	return float64(v&fiftyThreeOnes) / fiftyThreeZeros
}

func (t *RendezvousBalancer) next(dataHash []byte, balancer *Balancer) int {
	maxScore := 0.0
	idx := -1
	for _, node := range t.nodes {

		fnvHash := fnv.New64()
		_, _ = fnvHash.Write(dataHash)
		_, _ = fnvHash.Write(node.Seed)

		sum := intToFloat(fnvHash.Sum64())

		score := node.Weight * (1.0 / -math.Log2(sum))
		if !balancer.Backends[node.Idx].Alive.Load() {
			score *= -1
		}

		if score > maxScore {
			maxScore = score
			idx = node.Idx
		}
	}

	return idx
}

func (t *RendezvousBalancer) FillData(messages []io.RecvMmsgData, cnt int, balancer *Balancer) {
	for i := 0; i < cnt; i++ {
		var balancingData []byte

		if messages[i].Addr.Addr.Family == syscall.AF_INET {
			addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(&messages[i].Addr))
			balancingData = append(balancingData, addr.Addr[:]...)
		} else if messages[i].Addr.Addr.Family == syscall.AF_INET6 {
			addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(&messages[i].Addr))
			balancingData = append(balancingData, addr.Addr[:]...)
		} else {
			Log.Error("Incorrect socket family")
			continue
		}

		if balancer.Ipfix {
			// Version               16
			// Length                16
			// Export Time           32
			// Sequence Number       32
			// Observation Domain ID 32
			if !bytes.Equal(messages[i].Data[:2], []byte{0, 10}) {
				Log.Error("Invalid ipfix packet version")
			} else {
				balancingData = append(balancingData, messages[i].Data[12:16]...)
			}
		}

		dataHash := fnv.New64()
		_, _ = dataHash.Write(balancingData)

		backendIdx := t.next(dataHash.Sum(nil), balancer)
		if backendIdx < 0 {
			Log.Error("Could not select backend")
			continue
		}

		msgIdx := balancer.QueueByBackends[backendIdx].EnqCount

		balancer.QueueByBackends[backendIdx].Data[msgIdx] = messages[i]
		balancer.QueueByBackends[backendIdx].EnqCount++
	}
}
