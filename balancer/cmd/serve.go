package cmd

import (
	"balancer/internal/backend"
	"balancer/internal/balancer"
	appConfig "balancer/internal/config"
	"balancer/internal/health"
	"balancer/internal/io"
	"balancer/internal/util"
	"fmt"
	"math"
	"sync"

	"github.com/alexgaas/underdog"
	"github.com/gofrs/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/atomic"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the L4 balancer",
	Long:  `Start the high performance L4 load balancer with the specified configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		runBalancer()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runBalancer() {
	bs := viper.GetInt("buffer-size")
	if bs > 1024 {
		Log.Errorf("Buffer size is too big, using 1024")
		bs = 1024
	}

	stopChan := make(chan interface{})
	wg := &sync.WaitGroup{}

	config, err := appConfig.ReadConfig(viper.GetString("config"))
	if err != nil {
		Log.Fatal("Error reading config", log.Error(err))
	}
	backendsChecker := balancer.NewChecker()
	weightsChecker := balancer.NewWeightsChecker(viper.GetString("weights-file"), viper.GetInt("weights-timeout"))

	balancerUUIDsMap := make(map[string]interface{})
	for _, server := range config.Servers {

		backendsGroup := io.Broadcaster{}
		backendsGroup.Senders = make([]io.Sender, len(server.Broadcaster))

		for i, v := range server.Broadcaster {
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
					if len(configBackend.UUID) == 0 {
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
						Verbose:         viper.GetBool("verbose"),

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
					Verbose:         viper.GetBool("verbose"),

					NewWeight: atomic.NewFloat64(0),
				}
			}
		}

		if err := io.StartUDPListener(stopChan, wg, fmt.Sprintf("[::]:%d", server.Port), backendsGroup, viper.GetBool("verbose")); err != nil {
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
