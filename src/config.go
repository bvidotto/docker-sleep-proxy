package main

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ProxyPort       string
	TargetService   string
	TargetPort      string
	SleepTimeout    time.Duration
	CheckInterval   time.Duration
	EndpointPrefix  string
	AllowListMode   bool
	DockerHost      string
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func LoadConfig() Config {
	targetService := os.Getenv("TARGET_SERVICE")
	if targetService == "" {
		panic("TARGET_SERVICE environment variable is required")
	}

	targetPort := os.Getenv("TARGET_PORT")
	if targetPort == "" {
		panic("TARGET_PORT environment variable is required")
	}

	sleepTimeoutSec := getEnvInt("SLEEP_TIMEOUT", 86400)   // 24 hours default
	checkIntervalSec := getEnvInt("CHECK_INTERVAL", 5)     // 5 seconds default

	return Config{
		ProxyPort:       getEnv("PROXY_PORT", "8000"),
		TargetService:   targetService,
		TargetPort:      targetPort,
		SleepTimeout:    time.Duration(sleepTimeoutSec) * time.Second,
		CheckInterval:   time.Duration(checkIntervalSec) * time.Second,
		EndpointPrefix:  getEnv("ENDPOINT_PREFIX", "sleep-proxy"),
		AllowListMode:   getEnvBool("ALLOW_LIST_MODE", false),
		DockerHost:      getEnv("DOCKER_HOST", ""),
	}
}
