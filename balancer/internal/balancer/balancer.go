package balancer

import (
	"balancer/internal/backend"
	"balancer/internal/io"
	"fmt"
	"strings"

	"github.com/mitchellh/copystructure"
	"go.uber.org/atomic"

	"github.com/alexgaas/underdog"
)

var Log log.Logger

type BalancerAlgorithmPolicy uint8

const (
	// BalancerAlgoRR 0 for case when we forget to check error from algo parser
	BalancerAlgoRR = iota + 1
	BalancerAlgoRH
)

var BalancerAlgorithmString = map[string]uint8{
	"rr": BalancerAlgoRR,
	"rh": BalancerAlgoRH,
}

func ParseBalancerAlgorithm(value string) (BalancerAlgorithmPolicy, error) {
	if v, ok := BalancerAlgorithmString[strings.ToLower(value)]; ok {
		return BalancerAlgorithmPolicy(v), nil
	}

	return 0, fmt.Errorf("unknown algorithm %s", value)
}

type BalancerAlgorithm interface {
	Add(idx int, weight float64, seed []byte) error
	Update(idx int, weight float64) error
	FillData(messages []io.RecvMmsgData, cnt int, balancer *Balancer)
}

type QueueByBackendHolder struct {
	EnqCount int
	Data     []io.RecvMmsgData
}

type Balancer struct {
	Backends        []*backend.Backend
	QueueByBackends []*QueueByBackendHolder
	AlgorithmPolicy BalancerAlgorithmPolicy
	Algorithm       BalancerAlgorithm
	MaxRetry        int
	Ipfix           bool
	Checker         *Checker
	Alive           atomic.Bool
}

func (b *Balancer) InitState() error {
	if !b.IsAlive() {
		return fmt.Errorf("all backends are dead")
	}
	b.Checker.Add(b)

	switch b.AlgorithmPolicy {
	case BalancerAlgoRR:
		b.Algorithm = NewWrrBalancer()
	case BalancerAlgoRH:
		b.Algorithm = NewRendezvousBalancer()
	default:
		Log.Fatal("Unknown balancing algorithm")
	}

	for idx, backend := range b.Backends {
		if err := backend.InitState(); err != nil {
			return err
		}
		if err := b.Algorithm.Add(idx, backend.Weight, backend.Seed); err != nil {
			Log.Fatal("Could not add backend", log.Any("backend", backend.Host.Addr), log.Any("weight", backend.Weight), log.Error(err))
		}
	}

	return nil
}

func (b *Balancer) Enqueue(messages []io.RecvMmsgData, cnt int, isIPv6 bool) {
	if !b.Alive.Load() {
		Log.Error("Balancer is not alive")
	}

	markUpdate := false
	for k := range b.Backends {
		if b.Backends[k].Weight != b.Backends[k].NewWeight.Load() {
			markUpdate = true
		}
		b.QueueByBackends[k].EnqCount = 0
	}

	b.Algorithm.FillData(messages, cnt, b)

	for k := range b.Backends {
		if b.QueueByBackends[k].EnqCount > 0 {
			b.Backends[k].Enqueue(b.QueueByBackends[k].Data, b.QueueByBackends[k].EnqCount, isIPv6)
		}
	}

	// All goroutines have their own copy of backends.
	// It's safe to update weights here because all work is done.
	// No concurrent operations could occur here.
	if markUpdate {
		for k := range b.Backends {
			if b.Backends[k].Weight != b.Backends[k].NewWeight.Load() {
				newWeight := b.Backends[k].NewWeight.Load()
				if err := b.Algorithm.Update(k, newWeight); err == nil {
					b.Backends[k].Weight = newWeight
					Log.Info("Weight updated", log.Any("uuid", b.Backends[k].UUID), log.Any("weight", newWeight))
				} else {
					Log.Error("Weight updated failed", log.Any("uuid", b.Backends[k].UUID), log.Error(err))
				}
			}
		}
	}
}

func (b *Balancer) IsAlive() bool {
	failedCount := 0
	for _, backend := range b.Backends {
		if !backend.IsAlive() {
			failedCount++
		}
	}

	if failedCount == len(b.Backends) {
		b.Alive.Store(false)
	} else {
		b.Alive.Store(true)
	}

	return b.Alive.Load()
}

func (b *Balancer) Copy() io.Sender {
	backends := make([]*backend.Backend, len(b.Backends))

	//for idx, backend := range b.Backends {
	//	backends[idx] = backend.Copy().(*backend.Backend)
	//}

	queueTemp, _ := copystructure.Copy(b.QueueByBackends)
	holder := queueTemp.([]*QueueByBackendHolder)

	balancer := Balancer{
		Backends:        backends,
		QueueByBackends: holder,
		AlgorithmPolicy: b.AlgorithmPolicy,
		MaxRetry:        b.MaxRetry,
		Ipfix:           b.Ipfix,
		Checker:         b.Checker,
	}

	return &balancer
}
