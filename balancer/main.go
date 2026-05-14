package main

import (
	"balancer/internal/backend"
	"balancer/internal/balancer"
	appConfig "balancer/internal/config"
	"balancer/internal/health"
	"balancer/internal/io"
	"balancer/internal/util"
	"flag"
	"fmt"
	"math"
	"sync"

	"github.com/alexgaas/underdog"
	"github.com/alexgaas/underdog/zap"

	"github.com/gofrs/uuid"
	"go.uber.org/atomic"
	zp "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	config         = flag.String("config", "config.yaml", "yaml backends config")
	weightsFile    = flag.String("weights-file", "weights", "weights file")
	weightsTimeout = flag.Int("weights-timeout", 10, "weights file read timeout in seconds")
	bufferSize     = flag.Int("buffer-size", 100, "sendmmsg ring buffer size")
	verbose        = flag.Bool("verbose", false, "additional logging")
)

var Log = zap.Must(zp.Config{
	Level:            zp.NewAtomicLevelAt(zp.InfoLevel),
	Encoding:         "console",
	OutputPaths:      []string{"stdout"},
	ErrorOutputPaths: []string{"stderr"},
	EncoderConfig: zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "ts",
		CallerKey:      "caller",
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	},
})

func main() {
	flag.Parse()

	backend.Log = Log
	balancer.Log = Log
	balancer.Log = Log
	io.Log = Log

	if *bufferSize > 1024 {
		Log.Errorf("Buffer size is too big, using 1024")
		*bufferSize = 1024
	}

	stopChan := make(chan interface{})
	wg := &sync.WaitGroup{}

	config, err := appConfig.ReadConfig(*config)
	if err != nil {
		Log.Fatal("Error reading config", log.Error(err))
	}
	backendsChecker := balancer.NewChecker()
	weightsChecker := balancer.NewWeightsChecker(*weightsFile, *weightsTimeout)

	balancerUUIDsMap := make(map[string]interface{})
	for _, server := range config.Servers {

		backendsGroup := io.Broadcaster{}
		backendsGroup.Senders = make([]io.Sender, len(server.Broadcaster))

		for i, v := range server.Broadcaster {
			// We have two types of receivers
			// First is balancer
			if v.Balancer != nil {
				weightsSum := float64(0)
				weightsMin := math.MaxFloat64
				backendsList := make([]*backend.Backend, 0)

				for _, configBackend := range v.Balancer.Backends {
					if configBackend.CheckURL == "" {
						Log.Fatal("Empty CheckURL", log.Any("server port", server.Port))
					}

					hostInfo, err := backend.CreateHostSpecificInfo(configBackend.Addr, configBackend.ResolveOnlyIPV6, configBackend.FakeSrcIPv4Addr)
					if err != nil {
						Log.Fatal("Error resolving", log.Any("host", configBackend.Addr), log.Error(err))
					}

					var backendUUID string
					emptyBackendUUID := len(configBackend.UUID) == 0
					if emptyBackendUUID {
						newUUID, err := uuid.NewV4()
						if err != nil {
							Log.Fatal("Could not generate random uuid for balancer", log.Any("server port", server.Port), log.Error(err))
						}

						backendUUID = newUUID.String()
					} else {
						backendUUID = configBackend.UUID
					}

					if _, ok := balancerUUIDsMap[backendUUID]; ok {
						Log.Fatal("Duplicated uuid for backend", log.Any("server port", server.Port), log.Any("uuid", backendUUID))
					}
					balancerUUIDsMap[backendUUID] = true

					var seed []byte
					Log.Info("Generating new seed", log.Any("uuid", backendUUID))
					util.NewSeed(&seed)

					if len(seed) == 0 {
						Log.Fatal("Empty seed", log.Any("uuid", backendUUID))
					}

					balancerBackend := backend.Backend{
						UUID:            backendUUID,
						Host:            hostInfo,
						PreserveSrcAddr: configBackend.PreserveSrcAddr,
						ResolveOnlyIPV6: configBackend.ResolveOnlyIPV6,
						Verbose:         *verbose,

						Weight:   configBackend.Weight,
						Seed:     seed,
						CheckURL: fmt.Sprintf("http://%s", configBackend.CheckURL),

						NewWeight: atomic.NewFloat64(configBackend.Weight),
					}

					backendsList = append(backendsList, &balancerBackend)
					weightsSum += configBackend.Weight
					if weightsMin > configBackend.Weight {
						weightsMin = configBackend.Weight
					}

					weightsChecker.Add(&balancerBackend)
				}

				holder := make([]*balancer.QueueByBackendHolder, len(backendsList))
				for k := range backendsList {
					holder[k] = &balancer.QueueByBackendHolder{EnqCount: 0, Data: make([]io.RecvMmsgData, util.MaxReadFrames)}
				}

				var algo balancer.BalancerAlgorithmPolicy
				if v.Balancer.Algorithm == "" {
					algo = balancer.BalancerAlgoRR
					Log.Info("Using default balancing algorithm RR", log.Any("server port", server.Port))
				} else {
					algo, err = balancer.ParseBalancerAlgorithm(v.Balancer.Algorithm)
					if err != nil {
						Log.Fatal("Error parsing algorithm value", log.Any("server port", server.Port), log.Error(err))
					}
				}

				senderBalancer := &balancer.Balancer{
					Backends:        backendsList,
					QueueByBackends: holder,
					Ipfix:           v.Balancer.Ipfix,
					AlgorithmPolicy: algo,
					Checker:         backendsChecker,
					MaxRetry:        int(math.RoundToEven(weightsSum / weightsMin)),
				}

				backendsGroup.Senders[i] = senderBalancer
			}
			// The second one is plain backend
			if v.Backend != nil {
				b := v.Backend
				if b.CheckURL != "" {
					Log.Fatal("CheckURL without balancer", log.Any("server port", server.Port))
				}

				hostInfo, err := backend.CreateHostSpecificInfo(b.Addr, b.ResolveOnlyIPV6, b.FakeSrcIPv4Addr)
				if err != nil {
					Log.Fatal("Error resolving", log.Any("host", b.Addr), log.Error(err))
				}

				backendsGroup.Senders[i] = &backend.Backend{
					Host:            hostInfo,
					PreserveSrcAddr: b.PreserveSrcAddr,
					ResolveOnlyIPV6: b.ResolveOnlyIPV6,
					Verbose:         *verbose,

					NewWeight: atomic.NewFloat64(0),
				}

			}
		}

		if err := io.StartUDPListener(stopChan, wg, fmt.Sprintf("[::]:%d", server.Port), backendsGroup, *verbose); err != nil {
			Log.Fatal("Error starting server", log.Error(err))
		}
	}

	if err := backendsChecker.Start(stopChan, wg); err != nil {
		Log.Fatal("Error staring checker", log.Error(err))
	}

	weightsChecker.Start(stopChan, wg)

	health.StartStatusServer(fmt.Sprint("[::]:", config.StatusPort), backendsChecker)

	close(stopChan)
	wg.Wait()
}
