package machine

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codingsince1985/checksum"
	logging "github.com/ipfs/go-log"
	process "github.com/mudler/go-processmanager"
	"github.com/phayes/freeport"
	"github.com/spectrocloud/peg/internal/signals"
	"github.com/spectrocloud/peg/pkg/machine/internal/utils"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

var log = logging.Logger("machine")

func checksumErr(got, expected string) error {
	return fmt.Errorf("checksum mismatch: got %s, expected %s", got, expected)
}

func prepare(mc *types.MachineConfig) error {
	if mc.ID == "" {
		mc.ID = RandStringRunes(10)
		log.Infof("Automatically generated machine with id: %s", mc.ID)
	}

	if mc.StateDir == "" {
		f, err := os.MkdirTemp("", "peg")
		if err != nil {
			return err
		}
		mc.StateDir = f
		signals.AddCleanupFn(func() {
			log.Debug("Cleaning", f)
			os.RemoveAll(f)
		})
	}

	if mc.SSH.Port == "" {
		port, err := freeport.GetFreePort()
		if err != nil {
			return err
		}
		mc.SSH.Port = fmt.Sprint(port)
		log.Infof("Automatically generated local SSH port: %s", mc.SSH.Port)
	}

	if utils.IsValidURL(mc.ISO) {
		if mc.ISOChecksum == "" {
			log.Warn("!! Missing ISO checksum. It is strongly suggested to use a checksum")
		}
		dst := filepath.Join(mc.StateDir, fmt.Sprintf("%s.iso", RandStringRunes(10)))
		err := utils.Download(mc.ISO, dst)
		if err != nil {
			return err
		}
		mc.ISO = dst
		log.Infof("Automatically downloaded ISO: %s", mc.ISO)
		if mc.ISOChecksum != "" {
			log.Infof("Checksum for ISO present: %s", mc.ISOChecksum)

			var alg, hash string
			pieces := strings.Split(mc.ISOChecksum, ":")
			if len(pieces) == 1 {
				alg = "sha256"
				hash = mc.ISOChecksum
			} else if len(pieces) == 2 {
				alg = pieces[0]
				hash = pieces[1]
			}

			switch strings.ToLower(alg) {
			case "blake2s256":
				calcSha, err := checksum.Blake2s256(dst)
				if err != nil {
					return err
				}
				if calcSha != hash {
					return checksumErr(calcSha, hash)
				}
			case "md5":
				calcSha, err := checksum.MD5sum(dst)
				if err != nil {
					return err
				}
				if calcSha != hash {
					return checksumErr(calcSha, hash)
				}
			case "sha256":
				calcSha, err := checksum.SHA256sum(dst)
				if err != nil {
					return err
				}
				if calcSha != hash {
					return checksumErr(calcSha, hash)
				}
			}
		}
	}

	if utils.IsValidURL(mc.DataSource) {
		dst := filepath.Join(mc.StateDir, fmt.Sprintf("%s.iso", RandStringRunes(10)))
		err := utils.Download(mc.DataSource, dst)
		if err != nil {
			return err
		}
		mc.DataSource = dst
		log.Infof("Automatically downloaded additional ISO for the VM: %s", mc.DataSource)
	}

	return nil
}

func monitor(ctx context.Context, p *process.Process, f func(p *process.Process)) (context.Context, func()) {
	// resultCtx is done when the process exits or monitoring is intentionally
	// stopped. watchCtx lets callers suppress failure handling before an expected
	// process termination, such as a hard power cycle.
	resultCtx, cancelResult := context.WithCancel(ctx)
	watchCtx, cancelWatch := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		defer cancelResult()
		defer close(done)
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				if !p.IsAlive() {
					code, err := p.ExitCode()
					if (err != nil || code != "0") && f != nil {
						f(p)
					}
					return
				}
			}
		}
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancelWatch()
			<-done
		})
	}

	return resultCtx, stop
}

// New returns a new machine.
func New(opts ...types.MachineOption) (types.Machine, error) {
	mc := types.DefaultMachineConfig()

	err := mc.Apply(opts...)
	if err != nil {
		return nil, err
	}

	if err := prepare(mc); err != nil {
		return nil, fmt.Errorf("failure while preparing: %w", err)
	}

	switch mc.Engine {
	case types.QEMU:
		return &QEMU{machineConfig: *mc}, nil
	case types.Docker:
		return &Docker{machineConfig: *mc}, nil
	case types.VBox:
		return &VBox{machineConfig: *mc}, nil
	}

	return nil, fmt.Errorf("invalid engine: %s, obj: %+v", mc.Engine, mc)
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	bytes := make([]byte, n)
	// crypto/rand.Read never fails in practice on modern systems
	rand.Read(bytes)
	for i := range b {
		b[i] = letterRunes[int(bytes[i])%len(letterRunes)]
	}
	return string(b)
}
