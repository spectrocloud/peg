package peg

import (
	"fmt"
	"io/ioutil"
	"os"

	logging "github.com/ipfs/go-log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud/peg/internal/signals"
	"github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"gopkg.in/yaml.v3"
)

var log = logging.Logger("runner")

// Run runs peg files
func Run(f string, opts ...Option) error {

	fi, err := os.Stat(f)
	if err != nil {
		return fmt.Errorf("error while opening '%s': %w", f, err)
	}

	if f == "" {
		return fmt.Errorf("no file passed")
	}

	if fi.IsDir() {
		return fmt.Errorf("peg can run only files")
	}

	syncedFailer := SyncedFailer()

	o := &Options{
		Workers: 1,
	}
	for _, oo := range opts {
		if err := oo(o); err != nil {
			return err
		}
	}

	signals.HandleStopSignals()

	c := &Config{
		Clean: true,
	}

	var dat []byte
	if f == "-" {
		dat, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("can't read input file: %w", err)
		}
	} else {
		dat, err = ioutil.ReadFile(f)
		if err != nil {
			return fmt.Errorf("can't read input file: %w", err)
		}
	}

	if err := yaml.Unmarshal(dat, c); err != nil {
		return err
	}

	m, err := machine.New(append([]types.MachineOption{FromData(dat)}, o.MachineOptions...)...)
	if err != nil {
		return err
	}

	signals.AddCleanupFn(func() {
		m.Stop()
		m.Clean()
	})

	matcher.Machine = m
	//signal.Reset()

	err = Generate(c)
	if err != nil {
		return fmt.Errorf("while generating specs for '%s': %w", f, err)
	}
	//defer GinkgoRecover()

	suite, reporter := GinkgoConfiguration()
	suite.FailFast = o.FailFast
	suite.LabelFilter = o.LabelFilter
	suite.EmitSpecProgress = o.EmitSpecProgress
	suite.DryRun = o.DryRun
	suite.FlakeAttempts = o.FlakeAttempts
	suite.Timeout = o.Timeout
	suite.ParallelProcess = o.Workers
	suite.ParallelTotal = o.Workers

	reporter.Verbose = o.Verbose
	reporter.VeryVerbose = o.VeryVerbose
	reporter.AlwaysEmitGinkgoWriter = o.AlwaysEmitGinkgoWriter
	reporter.JUnitReport = o.JUnitReport
	reporter.JSONReport = o.JSONReport
	reporter.NoColor = o.NoColor
	reporter.SlowSpecThreshold = o.SlowSpecThreshold
	reporter.Succinct = o.Succint

	RegisterFailHandler(Fail)

	RunSpecs(syncedFailer, fmt.Sprintf("PEG: %s", f), suite, reporter)
	if syncedFailer.Failed() {
		return fmt.Errorf("failed running suites")
	}
	return nil
}
