package balancer

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type Checker struct {
	Balancers []*Balancer
}

func NewChecker() *Checker {
	return &Checker{[]*Balancer{}}
}

func (c *Checker) Start(stopChan chan interface{}, wg *sync.WaitGroup) error {
	for _, balancer := range c.Balancers {
		if !balancer.IsAlive() {
			return errors.New("all backends are dead")
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-stopChan:
				return
			default:
			}

			time.Sleep(10 * time.Second)

			for _, balancer := range c.Balancers {
				balancer.IsAlive()
			}
		}
	}()

	return nil
}

func (c *Checker) Add(balancer *Balancer) {
	c.Balancers = append(c.Balancers, balancer)
}

func (c *Checker) Status(w io.Writer) {
	for idx, balancer := range c.Balancers {

		if _, err := fmt.Fprintf(w, "Balancer idx %d status: %t\n", idx, balancer.Alive.Load()); err != nil {
			Log.Error("Could not write backends status")
		}

		for _, backend := range balancer.Backends {
			if _, err := fmt.Fprintf(w, "\tuuid %s url %s is alive %t weight %f\n", backend.UUID, backend.CheckURL, backend.Alive.Load(), backend.Weight); err != nil {
				Log.Error("Could not write backends status")
			}
		}
	}
}
