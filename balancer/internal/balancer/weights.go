package balancer

import (
	"balancer/internal/backend"
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexgaas/underdog"
)

type WeightsChecker struct {
	WeightsFile    string
	WeightsTimeout time.Duration

	uuidMapping map[string]*backend.Backend
}

func (c *WeightsChecker) Start(stopChan chan interface{}, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-stopChan:
				return
			default:
			}

			time.Sleep(c.WeightsTimeout)

			weightsFile, err := os.Open(c.WeightsFile)
			if err != nil {
				continue
			} else {
				Log.Info("Found weights file. Starting update")
			}

			weightsReader := bufio.NewScanner(weightsFile)

			for weightsReader.Scan() {
				line := weightsReader.Text()

				splitted := strings.Split(line, " ")

				if len(splitted) != 2 {
					Log.Error("Incorrect weights line", log.Any("line", line))
					continue
				}

				backendUUID := strings.Trim(splitted[0], " ")
				backendWeightString := strings.Trim(splitted[1], " ")

				backendWeight, err := strconv.ParseFloat(backendWeightString, 64)
				if err != nil {
					Log.Error("Could not parse weight", log.Any("line", line))
					continue
				}

				if backend, ok := c.uuidMapping[backendUUID]; ok {
					if backend.NewWeight.Load() != backendWeight {
						Log.Info("Found new backend weight",
							log.Any("backend", backendUUID),
							log.Any("old value", backend.NewWeight.Load()),
							log.Any("new value", backendWeight))

						backend.NewWeight.Store(backendWeight)
					}
				} else {
					Log.Error("Could not find backend", log.Any("backend", backendUUID))
				}
			}

			_ = weightsFile.Close()
		}
	}()
}

func (c WeightsChecker) Add(backend *backend.Backend) {
	c.uuidMapping[backend.UUID] = backend
}

func NewWeightsChecker(file string, timeout int) *WeightsChecker {
	return &WeightsChecker{
		WeightsFile:    file,
		WeightsTimeout: time.Duration(timeout) * time.Second,
		uuidMapping:    make(map[string]*backend.Backend),
	}
}
