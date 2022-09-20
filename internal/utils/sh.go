package utils

import (
	"os/exec"

	logging "github.com/ipfs/go-log"
)

// SH is a convenience wrapper over sh
func SH(c string) (string, error) {
	logging.Logger("sh").Debugf("Executing sh command: %s", c)
	o, err := exec.Command("/bin/sh", "-c", c).CombinedOutput()
	return string(o), err
}
