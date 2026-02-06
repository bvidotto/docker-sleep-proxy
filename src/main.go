package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/client"
)

type SleepProxy struct {
	config       Config
	dockerClient *client.Client
	projectName  string
	containerID  string
	lastActivity time.Time
	mu           sync.RWMutex
	containersUp bool
}

func NewSleepProxy(config Config) (*SleepProxy, error) {
	// Set DOCKER_HOST if provided in config
	if config.DockerHost != "" {
		os.Setenv("DOCKER_HOST", config.DockerHost)
		log.Printf("Using Docker host: %s", config.DockerHost)
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Get current container ID
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Get container info to find project name
	ctx := context.Background()
	containerJSON, err := dockerClient.ContainerInspect(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	projectName := containerJSON.Config.Labels["com.docker.compose.project"]
	if projectName == "" {
		return nil, fmt.Errorf("could not determine compose project name")
	}

	log.Printf("Sleep Proxy initialized for project: %s", projectName)
	log.Printf("Container ID: %s", hostname[:12])
	log.Printf("Target: %s:%s", config.TargetService, config.TargetPort)
	log.Printf("Sleep timeout: %v", config.SleepTimeout)

	sp := &SleepProxy{
		config:       config,
		dockerClient: dockerClient,
		projectName:  projectName,
		containerID:  hostname,
		lastActivity: time.Now(),
		containersUp: true,
	}

	// Check if target containers are actually running
	containers, err := sp.getProjectContainers(ctx)
	if err == nil {
		log.Printf("Found %d managed containers", len(containers))
		for _, c := range containers {
			log.Printf("Container: %s, State: %s", c.Names[0], c.State)
		}
		allRunning := true
		for _, c := range containers {
			if c.State != "running" {
				allRunning = false
				break
			}
		}
		sp.containersUp = allRunning
		if allRunning {
			log.Printf("Target containers are already running")
			
			// Apply startup behavior
			if config.StartupBehavior == "off" {
				log.Printf("Startup behavior is 'off' - stopping containers immediately")
				if err := sp.stopContainers(ctx); err != nil {
					log.Printf("Warning: Failed to stop containers on startup: %v", err)
				} else {
					sp.containersUp = false
					log.Printf("Containers stopped successfully")
				}
			} else {
				log.Printf("Startup behavior is 'timeout' - containers will sleep after %v of inactivity", config.SleepTimeout)
			}
		} else {
			log.Printf("Target containers are stopped")
		}
	}

	return sp, nil
}

func (sp *SleepProxy) setContainersUp(up bool) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.containersUp = up
}

func (sp *SleepProxy) areContainersUp() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.containersUp
}

func (sp *SleepProxy) updateActivity() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.lastActivity = time.Now()
}

func (sp *SleepProxy) getLastActivity() time.Time {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.lastActivity
}

func main() {
	config := LoadConfig()

	sleepProxy, err := NewSleepProxy(config)
	if err != nil {
		log.Fatalf("Failed to create sleep proxy: %v", err)
	}

	log.Printf("Sleep Proxy starting...")
	log.Printf("Listening on port: %s", config.ProxyPort)

	// Start activity monitor in background
	ctx := context.Background()
	go sleepProxy.monitorActivity(ctx)

	// Set up HTTP handlers
	sleepProxy.setupRoutes()

	// Start the HTTP server
	addr := ":" + config.ProxyPort
	log.Printf("Proxy server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
