package machine

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	proxmoxapi "github.com/luthermonson/go-proxmox"
	process "github.com/mudler/go-processmanager"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

func validProxmoxConfig() *types.ProxmoxConfig {
	return &types.ProxmoxConfig{
		APIURL:      "https://pve.example.test:8006/api2/json",
		Node:        "pve",
		TokenID:     "peg@pam!test",
		TokenSecret: "secret",
		Storage:     "local-lvm",
	}
}

func TestValidateProxmoxConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Proxmox, *types.ProxmoxConfig)
		wantErr string
	}{
		{name: "token authentication"},
		{name: "login authentication", mutate: func(_ *Proxmox, cfg *types.ProxmoxConfig) {
			cfg.TokenID, cfg.TokenSecret = "", ""
			cfg.Username, cfg.Password = "root@pam", "password"
		}},
		{name: "partial token", mutate: func(_ *Proxmox, cfg *types.ProxmoxConfig) { cfg.TokenSecret = "" }, wantErr: "requires both"},
		{name: "both authentication methods", mutate: func(_ *Proxmox, cfg *types.ProxmoxConfig) {
			cfg.Username, cfg.Password = "root@pam", "password"
		}, wantErr: "exactly one"},
		{name: "invalid URL", mutate: func(_ *Proxmox, cfg *types.ProxmoxConfig) { cfg.APIURL = "pve.local" }, wantErr: "absolute HTTP(S)"},
		{name: "args require login", mutate: func(p *Proxmox, _ *types.ProxmoxConfig) { p.machineConfig.Args = []string{"-netdev", "user"} }, wantErr: "require username/password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProxmoxConfig()
			p := &Proxmox{}
			if tt.mutate != nil {
				tt.mutate(p, cfg)
			}
			err := p.validateConfig(cfg)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("validateConfig() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validateConfig() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSDNSkipsRegularBridge(t *testing.T) {
	p := &Proxmox{}
	if err := p.validateSDN(context.Background(), &types.ProxmoxConfig{Bridge: "vmbr0"}); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareProxmoxCreatesStateAndSSHDefaults(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "nested", "state")
	cfg := &types.MachineConfig{StateDir: stateDir}
	if err := prepareProxmox(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ID == "" || cfg.SSH == nil || cfg.SSH.Port != "22" {
		t.Fatalf("unexpected prepared config: %#v", cfg)
	}
	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		t.Fatalf("state directory was not created: %v", err)
	}
}

func TestDiskSizeGiB(t *testing.T) {
	tests := map[string]int{"1": 1, "1024": 1, "1025": 2, "30000M": 30}
	for input, want := range tests {
		got, err := diskSizeGiB(input)
		if err != nil || got != want {
			t.Fatalf("diskSizeGiB(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0", "-1", "1G", "garbage"} {
		if _, err := diskSizeGiB(input); err == nil {
			t.Fatalf("diskSizeGiB(%q) unexpectedly succeeded", input)
		}
	}
}

func TestNextFreeSCSIIndex(t *testing.T) {
	config := &proxmoxapi.VirtualMachineConfig{
		SCSI0: "local-lvm:vm-100-disk-0",
		SCSI2: "local-lvm:vm-100-disk-2",
	}
	got, err := nextFreeSCSIIndex(config)
	if err != nil || got != 1 {
		t.Fatalf("nextFreeSCSIIndex() = %d, %v; want 1", got, err)
	}

	full := &proxmoxapi.VirtualMachineConfig{SCSIs: make(map[string]string)}
	for i := 0; i <= 30; i++ {
		full.SCSIs[fmt.Sprintf("scsi%d", i)] = "in-use"
	}
	if _, err := nextFreeSCSIIndex(full); err == nil {
		t.Fatal("nextFreeSCSIIndex() unexpectedly found a slot in a full configuration")
	}
	if _, err := nextFreeSCSIIndex(nil); err == nil {
		t.Fatal("nextFreeSCSIIndex(nil) unexpectedly succeeded")
	}
}

func TestBuildProxmoxVMOptions(t *testing.T) {
	cfg := validProxmoxConfig()
	cfg.Bridge = "vnet1"
	p := &Proxmox{machineConfig: types.MachineConfig{
		ID: "test", Memory: "2048", CPU: "2", CPUType: "x86-64-v2",
		DriveSizes: []string{"30000", "1025"}, ISO: "local:iso/test.iso",
	}}
	opts, err := p.buildVMOptions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]interface{}, len(opts))
	for _, option := range opts {
		got[option.Name] = option.Value
	}
	for name, want := range map[string]interface{}{
		"name": "peg-test", "cpu": "x86-64-v2", "net0": "virtio,bridge=vnet1",
		"scsi0": "local-lvm:30", "scsi1": "local-lvm:2",
		"ide2": "local:iso/test.iso,media=cdrom", "boot": "order=scsi0;ide2",
	} {
		if got[name] != want {
			t.Errorf("option %s = %#v, want %#v", name, got[name], want)
		}
	}
	p.machineConfig.DriveSizes = make([]string, 32)
	for i := range p.machineConfig.DriveSizes {
		p.machineConfig.DriveSizes[i] = "1024"
	}
	if _, err := p.buildVMOptions(cfg); err == nil {
		t.Fatal("buildVMOptions accepted more than 31 SCSI disks")
	}
}

func TestProxmoxLoginAuthentication(t *testing.T) {
	var ticketCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api2/json")
		switch {
		case r.Method == http.MethodPost && path == "/access/ticket":
			ticketCalls.Add(1)
			fmt.Fprint(w, `{"data":{"username":"root@pam","ticket":"test-ticket","CSRFPreventionToken":"csrf"}}`)
		case r.Method == http.MethodGet && path == "/nodes/pve/status":
			if cookie, err := r.Cookie("PVEAuthCookie"); err != nil || cookie.Value != "test-ticket" {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"data":{}}`)
		default:
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	p := &Proxmox{}
	cfg := validProxmoxConfig()
	cfg.APIURL = server.URL + "/api2/json"
	cfg.TokenID, cfg.TokenSecret = "", ""
	cfg.Username, cfg.Password = "root@pam", "password"
	if err := p.initClient(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := p.client.Node(context.Background(), "pve"); err != nil {
		t.Fatal(err)
	}
	if ticketCalls.Load() != 1 {
		t.Fatalf("ticket requests = %d, want 1", ticketCalls.Load())
	}
}

func TestProxmoxAPIEndpoint(t *testing.T) {
	for input, want := range map[string]string{
		"https://pve.example/api2/json":        "pve.example:8006",
		"https://pve.example:9443/api2/json":   "pve.example:9443",
		"https://[2001:db8::1]/api2/json":      "[2001:db8::1]:8006",
		"https://[2001:db8::1]:9443/api2/json": "[2001:db8::1]:9443",
	} {
		got, err := proxmoxAPIEndpoint(input)
		if err != nil || got != want {
			t.Errorf("proxmoxAPIEndpoint(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestServeISO(t *testing.T) {
	isoPath := filepath.Join(t.TempDir(), "image with spaces.iso")
	want := []byte("iso contents")
	if err := os.WriteFile(isoPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	serveURL, shutdown, err := serveISO(isoPath, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown() })

	response, err := http.Get(serveURL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("GET ISO = %q, %v; want %q", got, err, want)
	}

	request, _ := http.NewRequest(http.MethodPost, serveURL, nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestProxmoxISOModesAndCollisions(t *testing.T) {
	if !isProxmoxStorageRef("local:iso/image.iso") || isProxmoxStorageRef("https://example.test/image.iso") {
		t.Fatal("storage-reference ISO detection returned an unexpected result")
	}
	if alg, hash, err := parseChecksum("sha512:abc123"); err != nil || alg != "sha512" || hash != "abc123" {
		t.Fatalf("parseChecksum() = %q, %q, %v", alg, hash, err)
	}
	if _, _, err := parseChecksum("crc32:abc123"); err == nil {
		t.Fatal("parseChecksum accepted an unsupported algorithm")
	}

	contents := []*proxmoxapi.StorageContent{
		{Volid: "other:iso/peg-owned.iso", Size: 10},
		{Volid: "local:iso/unrelated.iso", Size: 10},
	}
	exists, err := pegISOExists(contents, "local", "peg-owned.iso", 10)
	if err != nil || exists {
		t.Fatalf("unrelated collision = %v, %v; want false, nil", exists, err)
	}

	contents = append(contents, &proxmoxapi.StorageContent{Volid: "local:iso/peg-owned.iso", Size: 10})
	exists, err = pegISOExists(contents, "local", "peg-owned.iso", 10)
	if err != nil || !exists {
		t.Fatalf("owned ISO match = %v, %v; want true, nil", exists, err)
	}
	if _, err := pegISOExists(contents, "local", "peg-owned.iso", 11); err == nil || !strings.Contains(err.Error(), "refusing to delete") {
		t.Fatalf("unsafe collision error = %v", err)
	}

	if got := sanitizeProxmoxName("../../my image.iso"); strings.ContainsAny(got, "/ ") {
		t.Fatalf("sanitizeProxmoxName() = %q, want a path-safe name", got)
	}
	firstName := proxmoxURLISOName("https://example.test/one/image.iso", "/one/image.iso")
	secondName := proxmoxURLISOName("https://example.test/two/image.iso", "/two/image.iso")
	if firstName == secondName || !strings.HasPrefix(firstName, "peg-") {
		t.Fatalf("URL ISO names are not Peg-owned and URL-addressed: %q, %q", firstName, secondName)
	}
}

func TestDownloadURLToProxmox(t *testing.T) {
	previousInterval := proxmoxapi.DefaultWaitInterval
	proxmoxapi.DefaultWaitInterval = time.Millisecond
	t.Cleanup(func() { proxmoxapi.DefaultWaitInterval = previousInterval })

	const isoURL = "https://images.example.test/releases/image.iso"
	var requestedFilename string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api2/json")
		switch {
		case r.Method == http.MethodGet && path == "/nodes/pve/status":
			fmt.Fprint(w, `{"data":{}}`)
		case r.Method == http.MethodPost && path == "/nodes/pve/storage/local/download-url":
			var options proxmoxapi.StorageDownloadURLOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
				t.Errorf("decode download options: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			requestedFilename = options.Filename
			if options.URL != isoURL || options.Content != "iso" || options.ChecksumAlgorithm != "sha256" {
				t.Errorf("unexpected download options: %#v", options)
			}
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:download:iso:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:download:iso:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:download:iso:root@pam:", "download"))
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+path, http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := validProxmoxConfig()
	cfg.APIURL = server.URL + "/api2/json"
	p := &Proxmox{machineConfig: types.MachineConfig{ID: "iso", ISOChecksum: "sha256:abc123"}}
	if err := p.initClient(cfg); err != nil {
		t.Fatal(err)
	}
	node, err := p.client.Node(context.Background(), "pve")
	if err != nil {
		t.Fatal(err)
	}
	p.node = node
	if err := p.downloadURLToProxmox(context.Background(), cfg, isoURL, "local"); err != nil {
		t.Fatal(err)
	}
	if requestedFilename == "" || p.machineConfig.ISO != "local:iso/"+requestedFilename {
		t.Fatalf("downloaded ISO = %q, filename = %q", p.machineConfig.ISO, requestedFilename)
	}
}

func TestRFBGrabFrame(t *testing.T) {
	stream := &bytes.Buffer{}
	stream.WriteString("RFB 003.008\n")
	stream.Write([]byte{1, rfbSecurityNone})
	_ = binary.Write(stream, binary.BigEndian, uint32(0))
	_ = binary.Write(stream, binary.BigEndian, uint16(1))
	_ = binary.Write(stream, binary.BigEndian, uint16(1))
	stream.Write(make([]byte, 16))
	_ = binary.Write(stream, binary.BigEndian, uint32(0))
	stream.Write([]byte{rfbMsgFramebufferUpdate, 0})
	_ = binary.Write(stream, binary.BigEndian, uint16(1))
	for _, value := range []uint16{0, 0, 1, 1} {
		_ = binary.Write(stream, binary.BigEndian, value)
	}
	_ = binary.Write(stream, binary.BigEndian, int32(rfbEncodingRaw))
	stream.Write([]byte{1, 2, 3, 0})

	send := make(chan []byte, 8)
	width, height, pixels, err := rfbGrabFrame(context.Background(), stream, send, "")
	if err != nil {
		t.Fatal(err)
	}
	if width != 1 || height != 1 || !bytes.Equal(pixels, []byte{1, 2, 3, 0}) {
		t.Fatalf("frame = %dx%d %v", width, height, pixels)
	}
	if len(send) != 6 {
		t.Fatalf("sent %d RFB messages, want 6", len(send))
	}
}

func TestRFBValidation(t *testing.T) {
	if _, err := pixelsToPPM(1, 1, []byte{1, 2, 3}); err == nil {
		t.Fatal("pixelsToPPM accepted a short pixel buffer")
	}
	if _, err := vncAuthEncrypt(make([]byte, 15), "password"); err == nil {
		t.Fatal("vncAuthEncrypt accepted a short challenge")
	}
	response, err := vncAuthEncrypt([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, "password")
	if err != nil {
		t.Fatal(err)
	}
	wantResponse := []byte{0xb8, 0x66, 0x92, 0x41, 0x25, 0xc8, 0xee, 0xbb, 0x9d, 0xeb, 0xc1, 0xdb, 0x61, 0xc5, 0x38, 0xe2}
	if !bytes.Equal(response, wantResponse) {
		t.Fatalf("VNC authentication response = %x, want %x", response, wantResponse)
	}
	malformed := &bytes.Buffer{}
	malformed.WriteString("RFB 003.008\n")
	malformed.WriteByte(0)
	_ = binary.Write(malformed, binary.BigEndian, uint32(maxRFBNameLength+1))
	if _, _, _, err := rfbGrabFrame(context.Background(), malformed, make(chan []byte, 1), ""); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("malformed RFB error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := sendRFB(ctx, make(chan []byte), []byte("blocked")); err == nil {
		t.Fatal("sendRFB unexpectedly succeeded")
	}
}

func TestMonitorVMWithoutInitializedVM(t *testing.T) {
	var callbackCalls atomic.Int32
	p := &Proxmox{machineConfig: types.MachineConfig{
		OnFailure: func(_ *process.Process) { callbackCalls.Add(1) },
	}}
	monitoredCtx := p.monitorVMWithInterval(context.Background(), time.Millisecond)
	select {
	case <-monitoredCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("monitor context was not canceled")
	}
	if callbackCalls.Load() != 0 {
		t.Fatalf("failure callback calls = %d, want 0", callbackCalls.Load())
	}
}

func TestStopMonitoringStopsPollingGoroutine(t *testing.T) {
	p := &Proxmox{}
	monitoredCtx := p.monitorVMWithInterval(context.Background(), time.Hour)
	stopped := make(chan struct{})
	go func() {
		p.stopMonitoring()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stopMonitoring did not synchronize with the polling goroutine")
	}
	if err := monitoredCtx.Err(); err != context.Canceled {
		t.Fatalf("monitor context error = %v, want context.Canceled", err)
	}
}

func TestMonitorVMToleratesTransientPingFailures(t *testing.T) {
	var statusCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api2/json")
		switch {
		case r.Method == http.MethodGet && path == "/nodes/pve/status":
			fmt.Fprint(w, `{"data":{}}`)
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/status/current":
			call := statusCalls.Add(1)
			if call == 2 || call == 3 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"data":{"Node":"pve","VMID":100,"Status":"running"}}`)
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/config":
			fmt.Fprint(w, `{"data":{}}`)
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+path, http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := validProxmoxConfig()
	cfg.APIURL = server.URL + "/api2/json"
	p := &Proxmox{vmid: 100}
	if err := p.initClient(cfg); err != nil {
		t.Fatal(err)
	}
	node, err := p.client.Node(context.Background(), "pve")
	if err != nil {
		t.Fatal(err)
	}
	p.node = node
	p.vm, err = node.VirtualMachine(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	monitoredCtx := p.monitorVMWithInterval(parentCtx, time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for statusCalls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if statusCalls.Load() < 4 {
		t.Fatalf("monitor made only %d status calls", statusCalls.Load())
	}
	if err := monitoredCtx.Err(); err != nil {
		t.Fatalf("monitor canceled after transient failures: %v", err)
	}
	cancel()
	select {
	case <-monitoredCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after parent cancellation")
	}
}

func TestProxmoxLifecycle(t *testing.T) {
	previousInterval := proxmoxapi.DefaultWaitInterval
	proxmoxapi.DefaultWaitInterval = time.Millisecond
	t.Cleanup(func() { proxmoxapi.DefaultWaitInterval = previousInterval })

	var status atomic.Int32
	var startCalls, stopCalls, deleteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "PVEAPIToken=peg@pam!test=secret" {
			t.Errorf("Authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api2/json")
		switch {
		case r.Method == http.MethodGet && path == "/nodes/pve/status":
			fmt.Fprint(w, `{"data":{}}`)
		case r.Method == http.MethodGet && path == "/cluster/status":
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && path == "/cluster/nextid":
			fmt.Fprint(w, `{"data":"100"}`)
		case r.Method == http.MethodPost && path == "/nodes/pve/qemu":
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmcreate:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmcreate:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmcreate:100:root@pam:", "qmcreate"))
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/status/current":
			vmStatus := "stopped"
			if status.Load() == 1 {
				vmStatus = "running"
			}
			fmt.Fprintf(w, `{"data":{"Node":"pve","VMID":100,"Status":%q}}`, vmStatus)
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/config":
			fmt.Fprint(w, `{"data":{"scsi0":"local-lvm:vm-100-disk-0,size=1G"}}`)
		case r.Method == http.MethodPost && path == "/nodes/pve/qemu/100/status/start":
			startCalls.Add(1)
			status.Store(1)
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmstart:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmstart:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmstart:100:root@pam:", "qmstart"))
		case r.Method == http.MethodPost && path == "/nodes/pve/qemu/100/status/stop":
			stopCalls.Add(1)
			status.Store(0)
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmstop:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmstop:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmstop:100:root@pam:", "qmstop"))
		case r.Method == http.MethodDelete && path == "/nodes/pve/qemu/100":
			deleteCalls.Add(1)
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmdestroy:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmdestroy:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmdestroy:100:root@pam:", "qmdestroy"))
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+path, http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := validProxmoxConfig()
	cfg.APIURL = server.URL + "/api2/json"
	p := &Proxmox{machineConfig: types.MachineConfig{
		ID: "lifecycle", CPU: "2", Memory: "2048", DriveSizes: []string{"1024"},
		SSH: &types.SSH{}, Proxmox: cfg, StateDir: stateDir,
	}}
	monitorCtx, err := p.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-monitorCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the monitor context")
	}
	if err := p.Clean(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state directory still exists: %v", err)
	}
	if startCalls.Load() != 1 || stopCalls.Load() != 1 || deleteCalls.Load() != 1 {
		t.Fatalf("lifecycle calls: start=%d stop=%d delete=%d; want 1 each", startCalls.Load(), stopCalls.Load(), deleteCalls.Load())
	}
}

func TestCreateRollsBackAfterStartFailure(t *testing.T) {
	previousInterval := proxmoxapi.DefaultWaitInterval
	proxmoxapi.DefaultWaitInterval = time.Millisecond
	t.Cleanup(func() { proxmoxapi.DefaultWaitInterval = previousInterval })

	var deleteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api2/json")
		t.Logf("mock Proxmox request: %s %s", r.Method, path)
		switch {
		case r.Method == http.MethodGet && path == "/nodes/pve/status":
			fmt.Fprint(w, `{"data":{}}`)
		case r.Method == http.MethodGet && path == "/cluster/status":
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && path == "/cluster/nextid":
			fmt.Fprint(w, `{"data":"100"}`)
		case r.Method == http.MethodPost && path == "/nodes/pve/qemu":
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmcreate:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmcreate:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmcreate:100:root@pam:", "qmcreate"))
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/status/current":
			fmt.Fprint(w, `{"data":{"status":"stopped","vmid":100}}`)
		case r.Method == http.MethodGet && path == "/nodes/pve/qemu/100/config":
			fmt.Fprint(w, `{"data":{"scsi0":"local-lvm:vm-100-disk-0,size=30G"}}`)
		case r.Method == http.MethodPost && path == "/nodes/pve/qemu/100/status/start":
			http.Error(w, "start failed", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && path == "/nodes/pve/qemu/100":
			deleteCalls.Add(1)
			fmt.Fprint(w, `{"data":"UPID:pve:1:1:1:qmdestroy:100:root@pam:"}`)
		case r.Method == http.MethodGet && strings.Contains(path, "/tasks/UPID:pve:1:1:1:qmdestroy:100:root@pam:/status"):
			fmt.Fprint(w, taskStatusJSON("UPID:pve:1:1:1:qmdestroy:100:root@pam:", "qmdestroy"))
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+path, http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := validProxmoxConfig()
	cfg.APIURL = server.URL + "/api2/json"
	p := &Proxmox{machineConfig: types.MachineConfig{
		ID: "rollback", CPU: "2", Memory: "2048", SSH: &types.SSH{}, Proxmox: cfg,
	}}
	_, err := p.Create(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("Create() error = %v, want start failure", err)
	}
	if deleteCalls.Load() != 1 {
		t.Fatalf("rollback DELETE calls = %d, want 1", deleteCalls.Load())
	}
}

func taskStatusJSON(upid, taskType string) string {
	return fmt.Sprintf(
		`{"data":{"UPID":%q,"Node":"pve","Type":%q,"ID":"100","User":"root@pam","Status":"stopped","ExitStatus":"OK"}}`,
		upid, taskType,
	)
}
