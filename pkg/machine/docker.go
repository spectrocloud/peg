package machine

import (
	"context"
	"fmt"
	"github.com/spectrocloud/peg/internal/utils"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"strings"
)

type Docker struct {
	machineConfig types.MachineConfig
}

func (q *Docker) whereIsDocker() string {
	processName := "/usr/bin/docker"
	if q.machineConfig.Process != "" {
		processName = q.machineConfig.Process
	}
	return processName
}

func (q *Docker) Create(ctx context.Context) error {
	log.Info("Create docker machine")

	processName := q.whereIsDocker()

	log.Infof("Starting Docker container with %s. Image: %s", processName, q.machineConfig.Image)

	cmd := fmt.Sprintf("%s run %s --entrypoint /bin/sh -d -t --name %s %s", processName, strings.Join(q.machineConfig.Args, " "), q.machineConfig.ID, q.machineConfig.Image)
	out, err := utils.SH(cmd)
	if err != nil {
		return fmt.Errorf("failed creating container: %w - cmd: %s, out: %s", err, cmd, out)
	}
	return nil
}

func (q *Docker) Config() types.MachineConfig {
	return q.machineConfig
}

func (q *Docker) Stop() error {
	out, err := utils.SH(fmt.Sprintf("%s stop %s", q.whereIsDocker(), q.machineConfig.ID))
	if err != nil {
		return fmt.Errorf("failed stopping container: %w - %s", err, out)
	}
	return nil
}

func (q *Docker) Clean() error {
	out, err := utils.SH(fmt.Sprintf("%s rm %s", q.whereIsDocker(), q.machineConfig.ID))
	if err != nil {
		return fmt.Errorf("failed deleting container: %w - %s", err, out)
	}
	out, err = utils.SH(fmt.Sprintf("%s rmi %s", q.whereIsDocker(), q.machineConfig.Image))
	if err != nil {
		log.Warn("failed deleting image: %w s %s", err.Error(), out)
	}
	return nil
}

func (q *Docker) Alive() bool {
	out, err := utils.SH(fmt.Sprintf("%s container inspect -f '{{.State.Running}}' %s", q.whereIsDocker(), q.machineConfig.Image))
	if err != nil {
		return false
	}
	if strings.Contains(out, "true") {
		return true
	}
	return false
}

// no-op
func (q *Docker) CreateDisk(diskname, size string) error {
	return nil
}

func (q *Docker) Command(cmd string) (string, error) {
	generatedCmd := fmt.Sprintf("%s exec %s /bin/sh -c '%s'", q.whereIsDocker(), q.machineConfig.ID, cmd)
	log.Infof("Running command: ", generatedCmd)

	return utils.SH(generatedCmd)
}

func (q *Docker) ReceiveFile(src, dst string) error {
	out, err := utils.SH(fmt.Sprintf("%s cp %s:%s %s", q.whereIsDocker(), q.machineConfig.ID, src, dst))
	if err != nil {
		return fmt.Errorf("failed receiving file from container: %w - %s", err, out)
	}
	return nil
}

func (q *Docker) SendFile(src, dst, permissions string) error {
	out, err := utils.SH(fmt.Sprintf("%s cp %s %s:%s", q.whereIsDocker(), src, q.machineConfig.ID, dst))
	if err != nil {
		return fmt.Errorf("failed receiving file from container: %w - %s", err, out)
	}
	return nil
}
