package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// The labels marking the containers this harness starts and the harness process that owns them.
const (
	browserContainerLabelKey   = "portcullis.test"
	browserContainerLabelValue = "browser"
	browserOwnerLabelKey       = "portcullis.test.owner"
)

type labelledContainer struct {
	id    string
	owner string
}

// staleContainerIDs returns the containers whose owner label does not name a live harness process.
func staleContainerIDs(containers []labelledContainer, isAlive func(pid int) bool) []string {
	var stale []string
	for _, container := range containers {
		pid, err := strconv.Atoi(container.owner)
		if err != nil || !isAlive(pid) {
			stale = append(stale, container.id)
		}
	}
	return stale
}

// removeStaleBrowserContainers deletes harness containers whose owning process has exited, leaving concurrent runs untouched.
func removeStaleBrowserContainers(ctx context.Context) error {
	listed, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label="+browserContainerLabelKey+"="+browserContainerLabelValue,
		"--format", "{{.ID}} {{.Label \""+browserOwnerLabelKey+"\"}}").Output()
	if err != nil {
		return err
	}
	var containers []labelledContainer
	for _, line := range strings.Split(strings.TrimSpace(string(listed)), "\n") {
		fields := strings.Fields(line)
		switch len(fields) {
		case 0:
		case 1:
			containers = append(containers, labelledContainer{id: fields[0]})
		default:
			containers = append(containers, labelledContainer{id: fields[0], owner: fields[1]})
		}
	}
	stale := staleContainerIDs(containers, isProcessAlive)
	if len(stale) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, "docker", append([]string{"rm", "-f"}, stale...)...).Run()
}

func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func harnessOwnerLabel() string {
	return strconv.Itoa(os.Getpid())
}
