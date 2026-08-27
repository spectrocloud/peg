package types

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWithFirmware(t *testing.T) {
	mc := DefaultMachineConfig()
	if err := mc.Apply(WithFirmware("/fw/hypervisor-fw")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if mc.Firmware != "/fw/hypervisor-fw" {
		t.Fatalf("got %q", mc.Firmware)
	}
	// empty value must not overwrite
	if err := mc.Apply(WithFirmware("")); err != nil {
		t.Fatalf("apply empty: %v", err)
	}
	if mc.Firmware != "/fw/hypervisor-fw" {
		t.Fatalf("empty overwrote firmware: %q", mc.Firmware)
	}
}

func TestCloudHypervisorEngineOption(t *testing.T) {
	mc := DefaultMachineConfig()
	if err := mc.Apply(CloudHypervisorEngine); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if mc.Engine != CloudHypervisor {
		t.Fatalf("got engine %q", mc.Engine)
	}
}

func TestEngineYAMLParse(t *testing.T) {
	mc := DefaultMachineConfig()
	in := []byte("engine: cloud-hypervisor\nfirmware: /fw/CLOUDHV.fd\n")
	if err := yaml.Unmarshal(in, mc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mc.Engine != CloudHypervisor {
		t.Fatalf("engine %q", mc.Engine)
	}
	if mc.Firmware != "/fw/CLOUDHV.fd" {
		t.Fatalf("firmware %q", mc.Firmware)
	}
}
