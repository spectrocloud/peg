package peg

import (
	"time"

	"github.com/spectrocloud/peg/pkg/machine/types"
)

type Options struct {
	Workers                                                                                  int
	FailFast                                                                                 bool
	EmitSpecProgress, DryRun, Verbose, VeryVerbose, AlwaysEmitGinkgoWriter, Succint, NoColor bool
	LabelFilter                                                                              string
	FlakeAttempts                                                                            int
	Timeout, SlowSpecThreshold                                                               time.Duration
	JUnitReport, JSONReport                                                                  string

	MachineOptions []types.MachineOption
}

type Option func(*Options) error

var Verbose Option = func(o *Options) error {
	o.Verbose = true
	return nil
}

var VeryVerbose Option = func(o *Options) error {
	o.VeryVerbose = true
	return nil
}

var Succint Option = func(o *Options) error {
	o.Succint = true
	return nil
}

var NoColor Option = func(o *Options) error {
	o.NoColor = true
	return nil
}

var AlwaysEmitGinkgoWriter Option = func(o *Options) error {
	o.AlwaysEmitGinkgoWriter = true
	return nil
}

var FailFast Option = func(o *Options) error {
	o.FailFast = true
	return nil
}

var EmitSpecProgress Option = func(o *Options) error {
	o.EmitSpecProgress = true
	return nil
}

var DryRun Option = func(o *Options) error {
	o.DryRun = true
	return nil
}

func WithMachineOptions(d ...types.MachineOption) Option {
	return func(o *Options) error {
		o.MachineOptions = append(o.MachineOptions, d...)
		return nil
	}
}

func WithWorkers(w int) Option {
	return func(o *Options) error {
		o.Workers = w
		return nil
	}
}

func WithJSONReport(d string) Option {
	return func(o *Options) error {
		o.JSONReport = d
		return nil
	}
}

func WithJUnitReport(d string) Option {
	return func(o *Options) error {
		o.JUnitReport = d
		return nil
	}
}

func WithLabelFilter(d string) Option {
	return func(o *Options) error {
		o.LabelFilter = d
		return nil
	}
}

func WithFlakeAttempts(w int) Option {
	return func(o *Options) error {
		o.FlakeAttempts = w
		return nil
	}
}

func WithTimeout(s string) Option {
	return func(o *Options) error {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		o.Timeout = dur
		return nil
	}
}

func WithSlowSpecThreshold(s string) Option {
	return func(o *Options) error {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		o.SlowSpecThreshold = dur
		return nil
	}
}
