package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Frontman-Labs/frontman"
	"github.com/Frontman-Labs/frontman/config"
	"github.com/Frontman-Labs/frontman/log"
)

func main() {

	// Define command-line flags
	var configFile string
	var logLevel string
	flag.StringVar(&configFile, "config", "", "path to configuration file")
	flag.StringVar(&logLevel, "log-level", "", "set log level to debug")

	// Parse command-line flags
	flag.Parse()

	// Load configuration from file or use default
	configPath := configFile
	if configPath == "" {
		configPath = "frontman.yaml"
	}

	config, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("failed to load configuration: %v", err)
		os.Exit(1)
	}

	if config.LoggingConfig.Level != "" && logLevel == "" {
		logLevel = config.LoggingConfig.Level
	} else {
		logLevel = "info"
	}
	logger, err := log.NewDefaultLogger(log.ParseLevel(logLevel))
	if err != nil {
		fmt.Println("failed to initialize logger")
		os.Exit(1)
	}

	// Create a new Gateway instance
	gateway, err := frontman.NewFrontman(config, logger)
	if err != nil {
		logger.Fatalf("failed to create gateway: %v", err)
	}

	// Start the server
	logger.Fatal(gateway.Start())
}
