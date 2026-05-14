package cmd

import (
	"balancer/internal/backend"
	"balancer/internal/balancer"
	"balancer/internal/io"
	"fmt"
	"os"

	"github.com/alexgaas/underdog/zap"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	zp "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	cfgFile        string
	weightsFile    string
	weightsTimeout int
	bufferSize     int
	verbose        bool
)

var Log *zap.Logger

var rootCmd = &cobra.Command{
	Use:   "balancer",
	Short: "L4 Balancer",
	Long:  `A high performance L4 load balancer.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initLogger)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "yaml backends config")
	rootCmd.PersistentFlags().StringVar(&weightsFile, "weights-file", "weights", "weights file")
	rootCmd.PersistentFlags().IntVar(&weightsTimeout, "weights-timeout", 10, "weights file read timeout in seconds")
	rootCmd.PersistentFlags().IntVar(&bufferSize, "buffer-size", 100, "sendmmsg ring buffer size")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "additional logging")

	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
	viper.BindPFlag("weights-file", rootCmd.PersistentFlags().Lookup("weights-file"))
	viper.BindPFlag("weights-timeout", rootCmd.PersistentFlags().Lookup("weights-timeout"))
	viper.BindPFlag("buffer-size", rootCmd.PersistentFlags().Lookup("buffer-size"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func initLogger() {
	Log = zap.Must(zp.Config{
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

	backend.Log = Log
	balancer.Log = Log
	io.Log = Log
}
