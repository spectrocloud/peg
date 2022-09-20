package main

import (
	"fmt"
	"os"

	logging "github.com/ipfs/go-log"
	"github.com/spectrocloud/peg/peg"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"github.com/urfave/cli"
)

func main() {

	app := &cli.App{
		Name:    "peg",
		Version: "0.1",
		Author:  "Ettore Di Giacinto",
		Usage:   "Opinionated VM test suite runner",
		Description: `
PEG is an openionated test suite runner, and a helper library based on Ginkgo which is focused to run on Containers and CI.

To run a peg spec, give the spec as argument to peg, or alternative with stdin:

$ cat <file.yaml> | peg -

is equivalent to:

$ peg <file.yaml>

You can override parts of the specs via CLI, for example, to override the iso from the spec you can:

$ peg --iso path_to_iso_file <file.yaml>

`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:   "label",
				Usage:  "run only assertions with label",
				EnvVar: "PEG_LABEL",
			},
			cli.StringFlag{
				Name:   "iso",
				Usage:  "overrides iso in peg specfiles",
				EnvVar: "PEG_ISO",
			},
			cli.StringFlag{
				Name:   "iso-checksum",
				Usage:  "overrides iso checksum in peg specfiles",
				EnvVar: "PEG_ISOCHECKSUM",
			},
			cli.StringFlag{
				Name:   "cpu",
				Usage:  "overrides cpu in peg specfiles",
				EnvVar: "PEG_CPU",
			},
			cli.StringFlag{
				Name:   "memory",
				Usage:  "overrides memory in peg specfiles",
				EnvVar: "PEG_MEMORY",
			},
			cli.StringFlag{
				Name:   "drive",
				Usage:  "overrides drive in peg specfiles",
				EnvVar: "PEG_DRIVE",
			},
			cli.StringFlag{
				Name:   "state",
				Usage:  "overrides state dir in peg specfiles",
				EnvVar: "PEG_STATE",
			},
			cli.StringFlag{
				Name:   "loglevel",
				Value:  "debug",
				Usage:  "loglevel",
				EnvVar: "PEG_LOGLEVEL",
			},
			cli.StringFlag{
				Name:   "json-report",
				Value:  "",
				Usage:  "Enables JSON reporting",
				EnvVar: "PEG_JSONREPORT",
			},
			cli.StringFlag{
				Name:   "junit-report",
				Value:  "",
				Usage:  "Enables JUnit reporting",
				EnvVar: "PEG_JUNITREPORT",
			},
			cli.StringFlag{
				Name:   "timeout",
				Value:  "",
				Usage:  "Test suite timeout",
				EnvVar: "PEG_TIMEOUT",
			},
			cli.StringFlag{
				Name:   "image",
				Value:  "",
				Usage:  "overide default image in peg file",
				EnvVar: "PEG_IMAGE",
			},
			cli.StringFlag{
				Name:   "slow-spec-threshold",
				Value:  "",
				Usage:  "Slow spec threshold",
				EnvVar: "PEG_SLOWSPECTHRESHOLD",
			},
			cli.IntFlag{
				Name:   "workers",
				Usage:  "Set number of workers",
				Value:  1,
				EnvVar: "PEG_WORKERS",
			},
			cli.IntFlag{
				Name:   "flake-attempts",
				Usage:  "Set Flake attempts",
				EnvVar: "PEG_FLAKEATTEMPTS",
			},
			cli.BoolFlag{
				Name:   "fail-fast",
				Usage:  "fail fast",
				EnvVar: "PEG_FAILFAST",
			},
			cli.BoolFlag{
				Name:   "dry-run",
				Usage:  "dry-run",
				EnvVar: "PEG_DRYRUN",
			},
			cli.BoolFlag{
				Name:   "emit-spec-progress",
				Usage:  "emit progress of specs",
				EnvVar: "PEG_EMITSPECPROGRESS",
			},
			cli.BoolFlag{
				Name:   "qemu",
				Usage:  "forces QEMU engine",
				EnvVar: "PEG_QEMU",
			},
			cli.BoolFlag{
				Name:   "verbose",
				Usage:  "verbose",
				EnvVar: "PEG_VERBOSE",
			},
			cli.BoolFlag{
				Name:   "very-verbose",
				Usage:  "very-verbose",
				EnvVar: "PEG_VERYVERBOSE",
			},
			cli.BoolFlag{
				Name:   "succint",
				Usage:  "succint",
				EnvVar: "PEG_SUCCINT",
			},
			cli.BoolFlag{
				Name:   "always-emit-writer",
				Usage:  "Always write",
				EnvVar: "PEG_ALWAYS_WRITE",
			},
			cli.BoolFlag{
				Name:   "no-color",
				Usage:  "Disables colored output",
				EnvVar: "PEG_NOCOLOR",
			},
			cli.BoolFlag{
				Name:   "vbox",
				Usage:  "forces VBox engine",
				EnvVar: "PEG_VBOX",
			},
		},
		UsageText: ``,
		Copyright: "Spectro Cloud",
		Action: func(c *cli.Context) error {
			lvl, err := logging.LevelFromString(c.String("loglevel"))
			if err != nil {
				panic(err)
			}
			logging.SetAllLoggers(lvl)

			f := c.Args().First()

			if _, err := os.Stat(".peg.yaml"); err == nil && f == "" {
				f = ".peg.yaml"
			}

			machineOpts := []types.MachineOption{
				types.WithCPU(c.String("cpu")),
				types.WithDrive(c.String("drive")),
				types.WithMemory(c.String("memory")),
				types.WithStateDir(c.String("state")),
				types.WithImage(c.String("image")),
				types.WithISO(c.String("iso")),
				types.WithISOChecksum(c.String("iso-checksum")),
			}

			if c.Bool("vbox") {
				machineOpts = append(machineOpts, types.VBoxEngine)
			}

			if c.Bool("qemu") {
				machineOpts = append(machineOpts, types.QEMUEngine)
			}

			pegOpts := []peg.Option{
				peg.WithLabelFilter(c.String("label")),
				peg.WithMachineOptions(machineOpts...),
				peg.WithWorkers(c.Int("workers")),
				peg.WithFlakeAttempts(c.Int("flake-attempts")),
				peg.WithJSONReport(c.String("json-report")),
				peg.WithJUnitReport(c.String("junit-report")),
			}

			if c.Bool("dry-run") {
				pegOpts = append(pegOpts, peg.DryRun)
			}

			if c.Bool("fail-fast") {
				pegOpts = append(pegOpts, peg.FailFast)
			}

			if c.Bool("verbose") {
				pegOpts = append(pegOpts, peg.Verbose)
			}

			if c.Bool("very-verbose") {
				pegOpts = append(pegOpts, peg.VeryVerbose)
			}

			if c.Bool("succint") {
				pegOpts = append(pegOpts, peg.Succint)
			}

			if c.Bool("always-emit-writer") {
				pegOpts = append(pegOpts, peg.AlwaysEmitGinkgoWriter)
			}

			if c.Bool("no-color") {
				pegOpts = append(pegOpts, peg.NoColor)
			}

			if c.Bool("emit-spec-progress") {
				pegOpts = append(pegOpts, peg.EmitSpecProgress)
			}

			if c.String("slow-spec-threshold") != "" {
				pegOpts = append(pegOpts, peg.WithSlowSpecThreshold(c.String("slow-spec-threshold")))
			}

			if c.String("timeout") != "" {
				pegOpts = append(pegOpts, peg.WithTimeout(c.String("timeout")))
			}

			return peg.Run(f,
				pegOpts...,
			)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
