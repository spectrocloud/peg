# peg

Peg my machine!

Peg ease out testing systems on VMs, docker and kubernetes. It is loosely based on the [ginkgo](https://github.com/onsi/ginkgo/) framework, and indeed offers a helper to be used with it.

It is intended to run either on Github actions, in a docker container or as a companion library for ginkgo/gomega BDD golang test based suites. So it either focuses on trying to run and be plugged in existing workflows - indeed its design is cut rather on pluggability than performance.

## Motivation

In the Open Source team at SpectroCloud we run our e2e test suites over Github. As we do intensive testing of OS images, we needed a solid foundation to collect common patterns. 
As we already use ginkgo as testing framework - and that seems to scale as well for testing VMs and systems, here we are with peg. A set of libraries and CLI that help into testing VMs, docker images and Kubernetes cluster. We couldn't find anything out there that simplifies creation of test systems, specifically focused at CIs environment and that could run on top of containers.

## Supported engines

- QEMU (no KVM)
- Docker
- Virtualbox

They share the same common apis, so you can control machine created with the engines in the same way from a testing perspective.

Design notes: QEMU has no KVM support to allow running QEMU machines inside docker containers. peg doesn't try to be smart by downloading all the required dependencies, instead it uses the smallest possible user-set from such, and tries to abstract from that to guarantee compatibilities between versions. 
Software like QEMU, Docker, and Virtualbox needs to be installed in the machine.

If you are running tests on Github, keep in mind that the Virtualbox engine is specifically tailored for it - you should just be good to go as is with no additional configuration.

## Usage

`peg` both support it's own syntax with yaml files, or either can be just used as a helper library to use with [ginkgo](https://github.com/onsi/ginkgo/).

### CLI

To run a peg spec, give the spec as argument to peg, or alternative with stdin:

```bash
$ cat <file.yaml> | peg -
$ peg <file.yaml>
```

You can override parts of the specs via CLI, for example, to override the iso from the spec you can:
```
$ peg --iso path_to_iso_file <file.yaml>
```

Example
```yaml
machine:
 engine: "qemu"
 iso: "https://github.com/c3os-io/c3os/releases/download/v0.57.0/c3os-alpine-v0.57.0.iso"
 ssh:
   user: "c3os"
   pass: "c3os"

specs:
- label: "download"
  describe: "bar"
  assertions:
   "Download":
    - preOps:
      - eventuallyConnects: 10
      command: |  
        echo aaa
      expect:
        containString: "aaa"
```

### As a library for tests

`peg` main use case is to use aside with `ginkgo` tests, however, it can also be used as a standard library to manage and control systems.

```go
import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    . "github.com/spectrocloud/peg/matcher"
    machine "github.com/spectrocloud/peg/pkg/machine"
)

Describe("Checking machine integrity", Label("integrity"), func() {
    // This could go as well in Before Suite
    BeforeEach(func() {
        m, err:= = machine.New(...)
        Expect(err).ToNot(HaveOccurred())
        Machine = m
        err := Machine.Create()
		Expect(err).ToNot(HaveOccurred())
    })

    // This could go also as AfterSuite
    AfterEach(func() {
        Machine.Stop()
		Machine.Clean()
    })

    When("the machine boots", func() {
        BeforeEach(func() {
           EventuallyConnects(120)
        })

        Context("and can run sudo commands", func() {
            It("returns the user name", func() {
                result, err:= Sudo("id")
 		        Expect(err).ToNot(HaveOccurred())
                Expect(result).To(ContainSubstring("root"))
            })
        })
    })
})
```

The machine can be controlled via the interface:

```golang

type Machine interface {
	Config() MachineConfig
	Create() error
	Stop() error
	Clean() error
	CreateDisk(diskname, size string) error
	Command(cmd string) (string, error)
	ReceiveFile(src, dst string) error
	SendFile(src, dst, permissions string) error
}

```

## License

Copyright (c) 2022 Spectro Cloud

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
