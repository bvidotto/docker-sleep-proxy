package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

func (sp *SleepProxy) getProjectContainers(ctx context.Context) ([]types.Container, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", sp.projectName))

	containers, err := sp.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	// Filter out the proxy itself and apply allowlist/denylist logic
	var projectContainers []types.Container
	for _, c := range containers {
		// Skip if it's the proxy container (check full or short ID)
		if c.ID == sp.containerID || (len(sp.containerID) >= 12 && c.ID[:12] == sp.containerID[:12]) {
			continue
		}
		
		labelValue := c.Labels["sleep-proxy.enable"]
		
		if sp.config.AllowListMode {
			// Allowlist mode: only include containers explicitly set to "true"
			if labelValue == "true" {
				projectContainers = append(projectContainers, c)
				log.Printf("Including container %s (allowlist mode, sleep-proxy.enable=%s)", c.Names[0], labelValue)
			}
		} else {
			// Denylist mode (default): include everything except containers set to "false"
			if labelValue != "false" {
				projectContainers = append(projectContainers, c)
			} else {
				log.Printf("Excluding container %s (denylist mode, sleep-proxy.enable=%s)", c.Names[0], labelValue)
			}
		}
	}

	return projectContainers, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (sp *SleepProxy) startContainers(ctx context.Context) error {
	containers, err := sp.getProjectContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	log.Printf("Starting %d containers in project '%s'", len(containers), sp.projectName)

	for _, c := range containers {
		if c.State != "running" {
			containerName := c.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
			log.Printf("Starting container: %s (state: %s)", containerName, c.State)
			if err := sp.dockerClient.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				log.Printf("Failed to start container %s: %v", containerName, err)
			} else {
				log.Printf("Successfully started: %s", containerName)
			}
		}
	}

	return nil
}

func (sp *SleepProxy) stopContainers(ctx context.Context) error {
	containers, err := sp.getProjectContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	log.Printf("Stopping %d containers in project '%s'", len(containers), sp.projectName)

	timeout := 10
	for _, c := range containers {
		if c.State == "running" {
			containerName := c.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
			log.Printf("Stopping container: %s", containerName)
			if err := sp.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
				log.Printf("Failed to stop container %s: %v", containerName, err)
			} else {
				log.Printf("Successfully stopped: %s", containerName)
			}
		}
	}

	sp.setContainersUp(false)
	return nil
}

func (sp *SleepProxy) monitorActivity(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	log.Printf("Activity monitor started (checking every 10 seconds)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check actual container state
			containers, err := sp.getProjectContainers(ctx)
			if err != nil {
				log.Printf("Failed to get project containers: %v", err)
				continue
			}

			// Determine if all containers are running
			allRunning := len(containers) > 0
			for _, c := range containers {
				if c.State != "running" {
					allRunning = false
					break
				}
			}

			// Update containersUp state if it's incorrect
			if allRunning && !sp.areContainersUp() {
				log.Printf("Detected containers are now running")
				sp.setContainersUp(true)
			} else if !allRunning && sp.areContainersUp() {
				log.Printf("Detected containers are no longer running")
				sp.setContainersUp(false)
			}

			// Check for timeout only if containers are running
			if sp.areContainersUp() {
				timeSinceActivity := time.Since(sp.getLastActivity())
				if timeSinceActivity > sp.config.SleepTimeout {
					log.Printf("No activity for %v (threshold: %v), putting containers to sleep",
						timeSinceActivity.Round(time.Second), sp.config.SleepTimeout)
					if err := sp.stopContainers(ctx); err != nil {
						log.Printf("Failed to stop containers: %v", err)
					} else {
						sp.setContainersUp(false)
						log.Printf("Containers stopped successfully")
					}
				}
			}
		}
	}
}
