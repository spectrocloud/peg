package peg

import (
	"io/ioutil"

	logging "github.com/ipfs/go-log"

	"github.com/spectrocloud/peg/pkg/machine/types"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Machine *types.MachineConfig `yaml:"machine,omitempty"`
	Clean   bool

	Tests []Test `yaml:"specs,omitempty"`
}

type Test struct {
	Label    string `yaml:"label,omitempty"`
	Describe string `yaml:"describe,omitempty"`

	Assertion map[string][]AssertionBlock `yaml:"assertions,omitempty"`
}

type AssertionBlock struct {
	Describe string      `yaml:"describe,omitempty"`
	Command  string      `yaml:"command,omitempty"`
	Expect   ExpectBlock `yaml:"expect,omitempty"`
	PreOps   []OpBlock   `yaml:"preOps,omitempty"`
	PostOps  []OpBlock   `yaml:"postOps,omitempty"`
	OnHost   bool        `yaml:"onHost,omitempty"`
}

type ExpectBlock struct {
	ContainSubstring string        `yaml:"containString,omitempty"`
	Equal            string        `yaml:"equal,omitempty"`
	Or               []ExpectBlock `yaml:"or,omitempty"`
	And              []ExpectBlock `yaml:"and,omitempty"`
	ToFail           bool          `yaml:"toFail,omitempty"`
	Not              bool          `yaml:"not,omitempty"`
}

type OpBlock struct {
	EventuallyConnect int               `yaml:"eventuallyConnects,omitempty"`
	SendFile          map[string]string `yaml:"sendFile,omitempty"`
	ReceiveFile       map[string]string `yaml:"receiveFile,omitempty"`
}

func (exp ExpectBlock) hasOrConditions() bool {
	return len(exp.Or) > 0
}

func (exp ExpectBlock) hasAndConditions() bool {
	return len(exp.And) > 0
}

func (exp ExpectBlock) isEqual() bool {
	return exp.Equal != ""
}

func (exp ExpectBlock) isContainSubString() bool {
	return exp.ContainSubstring != ""
}

func (exp ExpectBlock) Show(logger logging.StandardLogger) {
	showOr := func() {
		logger.Info("OR")
		for _, e := range exp.Or {
			e.Show(logger)
		}
	}

	showAnd := func() {
		logger.Info("AND")
		for _, e := range exp.And {
			e.Show(logger)
		}
	}

	if exp.isContainSubString() {
		logger.Infof("~> Containsubstring(%s)", exp.ContainSubstring)
		if exp.hasOrConditions() {
			showOr()
		} else if exp.hasAndConditions() {
			showAnd()
		}
	}

	if exp.isEqual() {
		logger.Infof("~> isEqual(%s)", exp.Equal)
		if exp.hasOrConditions() {
			showOr()
		} else if exp.hasAndConditions() {
			showAnd()
		}
	}
}

func (op OpBlock) Show(logger logging.StandardLogger) {
	if op.EventuallyConnect != 0 {
		logger.Infof("_ EventuallyConnect(%d)", op.EventuallyConnect)
	}
	if len(op.SendFile) > 0 {
		logger.Infof("_ SendFile(src: %s, dst: %s perms: %s)", op.SendFile["src"], op.SendFile["dst"], op.SendFile["permission"])
	}
	if len(op.ReceiveFile) > 0 {
		logger.Infof("_ ReceiveFile(src: %s, dst: %s)", op.ReceiveFile["src"], op.ReceiveFile["dst"])
	}
}

func (a AssertionBlock) Show(logger logging.StandardLogger) {
	logger.Infof("==> Assertion '%s' [ onhost: %t ]", a.Describe, a.OnHost)
	logger.Infof("== Pre operations")
	for _, op := range a.PreOps {
		op.Show(logger)
	}

	logger.Infof("== Test Command\n%s", a.Command)

	logger.Infof("== Expect")
	a.Expect.Show(logger)

	logger.Infof("== Post operations")
	for _, op := range a.PostOps {
		op.Show(logger)
	}
}

// FromFile populates a machineconfig from a peg config file.
func FromFile(f string) types.MachineOption {
	return func(mc *types.MachineConfig) error {

		c := &Config{
			Machine: &types.MachineConfig{
				AutoDriveSetup: true,
			},
		}

		d, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(d, c); err != nil {
			return err
		}
		*(mc) = *c.Machine

		return nil
	}
}

// FromFile populates a machineconfig from a peg config file.
func FromData(data []byte) types.MachineOption {
	return func(mc *types.MachineConfig) error {
		mm := types.DefaultMachineConfig()

		c := &Config{
			Machine: mm,
		}

		if err := yaml.Unmarshal(data, c); err != nil {
			return err
		}
		*(mc) = *c.Machine

		return nil
	}
}
