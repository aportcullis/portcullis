package main

import (
	"context"
	"os/exec"
	"strings"
)

// The label marking the containers this harness starts.
const (
	browserContainerLabelKey   = "portcullis.test"
	browserContainerLabelValue = "browser"
)

// removeStaleBrowserContainers deletes containers a killed harness left behind; call it only while holding the target port, so no running harness owns them.
func removeStaleBrowserContainers(ctx context.Context) error {
	listed, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label="+browserContainerLabelKey+"="+browserContainerLabelValue).Output()
	if err != nil {
		return err
	}
	containerIDs := strings.Fields(string(listed))
	if len(containerIDs) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, "docker", append([]string{"rm", "-f"}, containerIDs...)...).Run()
}
