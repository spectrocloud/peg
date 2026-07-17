package machine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	proxmoxapi "github.com/luthermonson/go-proxmox"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

const (
	proxmoxTaskTimeout   = 300 // seconds
	proxmoxAPITimeout    = 5 * time.Minute
	proxmoxCreateTimeout = 15 * time.Minute
	proxmoxPingTimeout   = 10 * time.Second

	proxmoxMonitorInterval     = 3 * time.Second
	proxmoxMonitorFailureLimit = 3
)

// Proxmox implements the Machine interface for Proxmox VE.
type Proxmox struct {
	machineConfig types.MachineConfig
	client        *proxmoxapi.Client
	node          *proxmoxapi.Node
	vm            *proxmoxapi.VirtualMachine
	vmid          int
	monitorCancel context.CancelFunc
	monitorDone   <-chan struct{}
}

func (p *Proxmox) Config() types.MachineConfig {
	return p.machineConfig
}

func (p *Proxmox) Create(ctx context.Context) (resultCtx context.Context, resultErr error) {
	log.Info("Create proxmox machine")
	operationCtx, cancelOperation := context.WithTimeout(ctx, proxmoxCreateTimeout)
	defer cancelOperation()

	cfg := p.machineConfig.Proxmox
	if cfg == nil {
		return ctx, fmt.Errorf("proxmox configuration is required")
	}

	if err := p.validateConfig(cfg); err != nil {
		return ctx, fmt.Errorf("invalid proxmox configuration: %w", err)
	}

	// Create Proxmox API client
	if err := p.initClient(cfg); err != nil {
		return ctx, fmt.Errorf("failed to initialize proxmox client: %w", err)
	}

	// Get the node
	node, err := p.client.Node(operationCtx, cfg.Node)
	if err != nil {
		return ctx, fmt.Errorf("failed to get proxmox node %q: %w", cfg.Node, err)
	}
	p.node = node

	// Handle ISO transfer to Proxmox storage if needed
	if err := p.prepareISO(operationCtx, cfg); err != nil {
		return ctx, fmt.Errorf("failed to prepare ISO: %w", err)
	}

	// Validate SDN infrastructure
	if err := p.validateSDN(operationCtx, cfg); err != nil {
		return ctx, fmt.Errorf("SDN validation failed: %w", err)
	}

	// Get next available VMID
	cluster, err := p.client.Cluster(operationCtx)
	if err != nil {
		return ctx, fmt.Errorf("failed to get cluster: %w", err)
	}

	vmid, err := cluster.NextID(operationCtx)
	if err != nil {
		return ctx, fmt.Errorf("failed to get next VMID: %w", err)
	}
	p.vmid = vmid

	log.Infof("Creating Proxmox VM %d on node %s [ Memory: %s, CPU: %s ]",
		vmid, cfg.Node, p.machineConfig.Memory, p.machineConfig.CPU)

	// Build VM creation options
	vmOpts, err := p.buildVMOptions(cfg)
	if err != nil {
		return ctx, fmt.Errorf("invalid Proxmox VM configuration: %w", err)
	}

	// Create the VM
	task, err := node.NewVirtualMachine(operationCtx, vmid, vmOpts...)
	if err != nil {
		return ctx, fmt.Errorf("failed to create VM %d: %w", vmid, err)
	}
	created := true
	defer func() {
		if resultErr == nil || !created {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), proxmoxAPITimeout)
		defer cancel()
		if rollbackErr := p.rollbackVM(rollbackCtx); rollbackErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("rolling back VM %d: %w", vmid, rollbackErr))
		}
	}()

	if err := task.WaitFor(operationCtx, proxmoxTaskTimeout); err != nil {
		return ctx, fmt.Errorf("VM creation task failed: %w", err)
	}

	log.Infof("VM %d created successfully", vmid)

	// Start the VM
	vm, err := node.VirtualMachine(operationCtx, vmid)
	if err != nil {
		return ctx, fmt.Errorf("failed to get VM %d after creation: %w", vmid, err)
	}
	p.vm = vm

	startTask, err := vm.Start(operationCtx)
	if err != nil {
		return ctx, fmt.Errorf("failed to start VM %d: %w", vmid, err)
	}

	if err := startTask.WaitFor(operationCtx, proxmoxTaskTimeout); err != nil {
		return ctx, fmt.Errorf("VM start task failed: %w", err)
	}

	log.Infof("VM %d started successfully", vmid)
	created = false

	// Start monitoring goroutine
	newCtx := p.monitorVM(ctx)

	return newCtx, nil
}

func (p *Proxmox) Stop() error {
	if p.vm == nil {
		return fmt.Errorf("VM not initialized")
	}

	p.stopMonitoring()
	ctx, cancel := context.WithTimeout(context.Background(), proxmoxAPITimeout)
	defer cancel()
	return p.stopVM(ctx)
}

func (p *Proxmox) stopVM(ctx context.Context) error {

	// Check if already stopped
	if err := p.vm.Ping(ctx); err != nil {
		return fmt.Errorf("failed to get VM status: %w", err)
	}

	if p.vm.IsStopped() {
		log.Info("VM is already stopped")
		return nil
	}

	task, err := p.vm.Stop(ctx)
	if err != nil {
		return fmt.Errorf("failed to stop VM %d: %w", p.vmid, err)
	}

	if err := task.WaitFor(ctx, proxmoxTaskTimeout); err != nil {
		return fmt.Errorf("VM stop task failed: %w", err)
	}

	log.Infof("VM %d stopped", p.vmid)
	return nil
}

func (p *Proxmox) Clean() error {
	if p.vm == nil {
		if p.machineConfig.StateDir != "" {
			return os.RemoveAll(p.machineConfig.StateDir)
		}
		return nil
	}

	p.stopMonitoring()
	ctx, cancel := context.WithTimeout(context.Background(), proxmoxAPITimeout)
	defer cancel()

	// Ensure VM is stopped first
	if err := p.vm.Ping(ctx); err == nil && !p.vm.IsStopped() {
		if err := p.stopVM(ctx); err != nil {
			log.Warnf("Failed to stop VM before cleanup: %v", err)
		}
	}

	// Delete the VM with purge to remove disks
	task, err := p.vm.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete VM %d: %w", p.vmid, err)
	}

	if err := task.WaitFor(ctx, proxmoxTaskTimeout); err != nil {
		return fmt.Errorf("VM delete task failed: %w", err)
	}

	log.Infof("VM %d deleted", p.vmid)
	p.vm = nil

	// Clean local state directory
	if p.machineConfig.StateDir != "" {
		return os.RemoveAll(p.machineConfig.StateDir)
	}

	return nil
}

func (p *Proxmox) Screenshot() (string, error) {
	if p.vm == nil {
		return "", fmt.Errorf("VM not initialized")
	}

	return p.screenshotVNC()
}

// screenshotVNC captures a screenshot via VNC WebSocket (API-only, no SSH).
func (p *Proxmox) screenshotVNC() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Get VNC proxy ticket
	vnc, err := p.vm.VNCProxy(ctx, &proxmoxapi.VNCConfig{Websocket: true})
	if err != nil {
		return "", fmt.Errorf("VNCProxy failed: %w", err)
	}

	// Connect WebSocket
	send, recv, errs, closer, err := p.vm.VNCWebSocket(vnc)
	if err != nil {
		return "", fmt.Errorf("VNCWebSocket failed: %w", err)
	}
	defer closer()

	// Grab a single frame via the RFB protocol
	reader := &wsChanReader{recv: recv, errs: errs, timeout: 10 * time.Second}
	width, height, pixels, err := rfbGrabFrame(ctx, reader, send, vnc.Ticket)
	if err != nil {
		return "", fmt.Errorf("RFB frame capture failed: %w", err)
	}

	// Convert to PPM and write to state dir
	ppm, err := pixelsToPPM(width, height, pixels)
	if err != nil {
		return "", fmt.Errorf("converting VNC frame: %w", err)
	}
	localPath := filepath.Join(p.machineConfig.StateDir, fmt.Sprintf("screenshot-%d.ppm", p.vmid))
	if err := os.WriteFile(localPath, ppm, 0644); err != nil {
		return "", fmt.Errorf("failed to write screenshot: %w", err)
	}

	log.Infof("VNC screenshot captured: %s (%dx%d)", localPath, width, height)
	return localPath, nil
}

func (p *Proxmox) CreateDisk(diskname, size string) error {
	if p.vm == nil {
		return fmt.Errorf("VM not initialized")
	}

	cfg := p.machineConfig.Proxmox
	if cfg == nil {
		return fmt.Errorf("proxmox configuration is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), proxmoxAPITimeout)
	defer cancel()

	sizeGB, err := diskSizeGiB(size)
	if err != nil {
		return err
	}

	// Find the next available SCSI slot
	scsiIdx, err := p.nextSCSIIndex(ctx)
	if err != nil {
		return fmt.Errorf("failed to find available SCSI slot: %w", err)
	}

	diskSpec := fmt.Sprintf("%s:%d", cfg.Storage, sizeGB)
	task, err := p.vm.Config(ctx, proxmoxapi.VirtualMachineOption{
		Name:  fmt.Sprintf("scsi%d", scsiIdx),
		Value: diskSpec,
	})
	if err != nil {
		return fmt.Errorf("failed to add disk: %w", err)
	}

	if err := task.WaitFor(ctx, proxmoxTaskTimeout); err != nil {
		return fmt.Errorf("disk creation task failed: %w", err)
	}

	log.Infof("Added disk %q as scsi%d (%dGB) to VM %d", diskname, scsiIdx, sizeGB, p.vmid)
	return nil
}

func (p *Proxmox) Command(cmd string) (string, error) {
	return controller.SSHCommand(p, cmd)
}

func (p *Proxmox) DetachCD() error {
	if p.vm == nil {
		return fmt.Errorf("VM not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), proxmoxAPITimeout)
	defer cancel()

	// Remove the IDE2 CD-ROM by setting it to none
	task, err := p.vm.Config(ctx, proxmoxapi.VirtualMachineOption{
		Name:  "ide2",
		Value: "none,media=cdrom",
	})
	if err != nil {
		return fmt.Errorf("failed to detach CD: %w", err)
	}

	if err := task.WaitFor(ctx, proxmoxTaskTimeout); err != nil {
		return fmt.Errorf("detach CD task failed: %w", err)
	}

	log.Infof("Detached CD from VM %d", p.vmid)
	return nil
}

func (p *Proxmox) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(p, src, dst)
}

func (p *Proxmox) SendFile(src, dst, permissions string) error {
	return controller.SendFile(p, src, dst, permissions)
}

// --- Private helpers ---

func (p *Proxmox) validateConfig(cfg *types.ProxmoxConfig) error {
	if cfg.APIURL == "" {
		return fmt.Errorf("proxmox apiURL is required")
	}
	if cfg.Node == "" {
		return fmt.Errorf("proxmox node is required")
	}
	parsedURL, err := url.ParseRequestURI(cfg.APIURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return fmt.Errorf("proxmox apiURL must be an absolute HTTP(S) URL")
	}
	if (cfg.TokenID == "") != (cfg.TokenSecret == "") {
		return fmt.Errorf("proxmox token authentication requires both tokenID and tokenSecret")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return fmt.Errorf("proxmox login authentication requires both username and password")
	}
	hasToken := cfg.TokenID != ""
	hasLogin := cfg.Username != ""
	if hasToken == hasLogin {
		return fmt.Errorf("proxmox auth requires exactly one of tokenID+tokenSecret or username+password")
	}
	if len(p.machineConfig.Args) > 0 && !hasLogin {
		return fmt.Errorf("custom Proxmox QEMU args require username/password authentication")
	}
	if cfg.Storage == "" {
		return fmt.Errorf("proxmox storage is required")
	}
	return nil
}

func (p *Proxmox) initClient(cfg *types.ProxmoxConfig) error {
	var authOpt proxmoxapi.Option
	if cfg.Username != "" && cfg.Password != "" {
		authOpt = proxmoxapi.WithLogins(cfg.Username, cfg.Password)
	} else {
		authOpt = proxmoxapi.WithAPIToken(cfg.TokenID, cfg.TokenSecret)
	}

	opts := []proxmoxapi.Option{authOpt}

	if cfg.InsecureTLS {
		opts = append(opts, proxmoxapi.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}))
	}

	p.client = proxmoxapi.NewClient(cfg.APIURL, opts...)
	return nil
}

// validateSDN checks that the expected SDN infrastructure exists.
// If the API token lacks SDN permissions, it logs a warning and continues
// rather than failing -- the user has already confirmed the SDN is configured.
func (p *Proxmox) validateSDN(ctx context.Context, cfg *types.ProxmoxConfig) error {
	// A bridge without an SDN zone is a regular Proxmox/Linux bridge (for
	// example vmbr0) and does not exist in the cluster SDN API.
	if cfg.Bridge == "" || cfg.Zone == "" {
		return nil
	}

	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		log.Warnf("Could not get cluster for SDN validation (will continue anyway): %v", err)
		return nil
	}

	// Validate zone exists.
	zone, err := cluster.SDNZone(ctx, cfg.Zone)
	if err != nil {
		log.Warnf("Could not validate SDN zone %q (may lack SDN.Audit permission): %v", cfg.Zone, err)
	} else {
		log.Infof("SDN zone %q found (type: %s)", cfg.Zone, zone.Type)
	}

	// Validate VNet exists
	vnet, err := cluster.SDNVNet(ctx, cfg.Bridge)
	if err != nil {
		log.Warnf("Could not validate SDN VNet %q (may lack SDN.Audit permission): %v", cfg.Bridge, err)
		return nil
	}
	log.Infof("SDN VNet %q found (zone: %s)", cfg.Bridge, vnet.Zone)

	// Validate VNet has a subnet with SNAT enabled
	subnets, err := cluster.SDNSubnets(ctx, cfg.Bridge)
	if err != nil {
		log.Warnf("Could not validate subnets for VNet %q: %v", cfg.Bridge, err)
		return nil
	}

	snatFound := false
	for _, subnet := range subnets {
		if subnet.SNAT != 0 {
			snatFound = true
			log.Infof("SDN subnet %s with SNAT enabled (gateway: %s)", subnet.CIDR, subnet.Gateway)
			break
		}
	}

	if !snatFound {
		log.Warnf("No subnet with SNAT enabled found on VNet %q; SNAT is required for outbound connectivity", cfg.Bridge)
	}

	return nil
}

func (p *Proxmox) buildVMOptions(cfg *types.ProxmoxConfig) ([]proxmoxapi.VirtualMachineOption, error) {
	// Default CPU type to "host" to pass through host CPU features.
	// Many modern distros require x86-64-v2 or higher which kvm64 doesn't provide.
	cpuType := "host"
	if p.machineConfig.CPUType != "" {
		cpuType = p.machineConfig.CPUType
	}

	opts := []proxmoxapi.VirtualMachineOption{
		{Name: "name", Value: fmt.Sprintf("peg-%s", p.machineConfig.ID)},
		{Name: "memory", Value: p.machineConfig.Memory},
		{Name: "cores", Value: p.machineConfig.CPU},
		{Name: "cpu", Value: cpuType},
		{Name: "scsihw", Value: "virtio-scsi-pci"},
	}

	// Bridge NIC is optional — when using SLIRP-only networking (via Args),
	// the bridge is omitted so the VM has a single NIC matching the cloud-config.
	if cfg.Bridge != "" {
		opts = append(opts, proxmoxapi.VirtualMachineOption{
			Name: "net0", Value: fmt.Sprintf("virtio,bridge=%s", cfg.Bridge),
		})
	}

	// Boot disk from DriveSizes
	driveSizes := p.machineConfig.DriveSizes
	if len(driveSizes) == 0 {
		driveSizes = []string{types.DefaultDriveSize}
	}
	if len(driveSizes) > 31 {
		return nil, fmt.Errorf("Proxmox supports at most 31 SCSI disks, got %d", len(driveSizes))
	}

	// Primary boot disk
	sizeGB, err := diskSizeGiB(driveSizes[0])
	if err != nil {
		return nil, err
	}
	opts = append(opts, proxmoxapi.VirtualMachineOption{
		Name:  "scsi0",
		Value: fmt.Sprintf("%s:%d", cfg.Storage, sizeGB),
	})

	// Additional drives
	for i := 1; i < len(driveSizes); i++ {
		sizeGB, err := diskSizeGiB(driveSizes[i])
		if err != nil {
			return nil, err
		}
		opts = append(opts, proxmoxapi.VirtualMachineOption{
			Name:  fmt.Sprintf("scsi%d", i),
			Value: fmt.Sprintf("%s:%d", cfg.Storage, sizeGB),
		})
	}

	// ISO (pre-staged on Proxmox storage)
	if p.machineConfig.ISO != "" {
		opts = append(opts, proxmoxapi.VirtualMachineOption{
			Name:  "ide2",
			Value: fmt.Sprintf("%s,media=cdrom", p.machineConfig.ISO),
		})
	}

	// Boot order: disk first, then CD
	bootOrder := "order=scsi0"
	if p.machineConfig.ISO != "" {
		bootOrder += ";ide2"
	}
	opts = append(opts, proxmoxapi.VirtualMachineOption{
		Name:  "boot",
		Value: bootOrder,
	})

	// Pass custom QEMU args (e.g., SLIRP NAT for port forwarding)
	if len(p.machineConfig.Args) > 0 {
		opts = append(opts, proxmoxapi.VirtualMachineOption{
			Name:  "args",
			Value: strings.Join(p.machineConfig.Args, " "),
		})
	}

	return opts, nil
}

func diskSizeGiB(size string) (int, error) {
	normalized := strings.TrimSpace(strings.TrimSuffix(strings.ToUpper(size), "M"))
	sizeMB, err := strconv.Atoi(normalized)
	if err != nil || sizeMB <= 0 {
		return 0, fmt.Errorf("invalid disk size %q: expected a positive number of MB", size)
	}
	return (sizeMB + 1023) / 1024, nil
}

// monitorVM starts a goroutine that polls VM status and cancels the context
// when the VM unexpectedly stops.
func (p *Proxmox) monitorVM(ctx context.Context) context.Context {
	return p.monitorVMWithInterval(ctx, proxmoxMonitorInterval)
}

func (p *Proxmox) monitorVMWithInterval(ctx context.Context, interval time.Duration) context.Context {
	monitorCtx, cancelFunc := context.WithCancel(ctx)
	done := make(chan struct{})
	p.monitorCancel = cancelFunc
	p.monitorDone = done
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer cancelFunc()
		defer close(done)
		failures := 0
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				if p.vm == nil {
					return
				}
				pingCtx, cancel := context.WithTimeout(monitorCtx, proxmoxPingTimeout)
				err := p.vm.Ping(pingCtx)
				cancel()
				if err != nil {
					if monitorCtx.Err() != nil {
						return
					}
					failures++
					log.Warnf("Failed to ping VM %d: %v", p.vmid, err)
					if failures >= proxmoxMonitorFailureLimit {
						return
					}
					continue
				}
				failures = 0
				if !p.vm.IsRunning() {
					log.Warnf("VM %d is no longer running (status: %s)", p.vmid, p.vm.Status)
					return
				}
			}
		}
	}()
	return monitorCtx
}

func (p *Proxmox) stopMonitoring() {
	if p.monitorCancel != nil {
		p.monitorCancel()
	}
	if p.monitorDone != nil {
		<-p.monitorDone
	}
	p.monitorCancel = nil
	p.monitorDone = nil
}

// nextSCSIIndex finds the next available SCSI device index by checking current config.
func (p *Proxmox) nextSCSIIndex(ctx context.Context) (int, error) {
	// Refresh VM config
	vm, err := p.node.VirtualMachine(ctx, p.vmid)
	if err != nil {
		return 0, err
	}
	p.vm = vm

	if vm.VirtualMachineConfig == nil {
		return 0, fmt.Errorf("VM %d returned no configuration", p.vmid)
	}
	return nextFreeSCSIIndex(vm.VirtualMachineConfig)
}

func nextFreeSCSIIndex(config *proxmoxapi.VirtualMachineConfig) (int, error) {
	if config == nil {
		return 0, fmt.Errorf("virtual machine configuration is required")
	}
	used := config.MergeSCSIs()
	for i := 0; i <= 30; i++ {
		if _, exists := used[fmt.Sprintf("scsi%d", i)]; !exists {
			return i, nil
		}
	}
	return 0, fmt.Errorf("virtual machine has no free SCSI slots")
}

func proxmoxAPIEndpoint(apiURL string) (string, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("invalid Proxmox API URL %q", apiURL)
	}
	port := parsed.Port()
	if port == "" {
		port = "8006"
	}
	return net.JoinHostPort(parsed.Hostname(), port), nil
}

func (p *Proxmox) rollbackVM(ctx context.Context) error {
	var upid proxmoxapi.UPID
	path := fmt.Sprintf("/nodes/%s/qemu/%d", p.machineConfig.Proxmox.Node, p.vmid)
	if err := p.client.Delete(ctx, path, &upid); err != nil {
		return err
	}
	task := proxmoxapi.NewTask(upid, p.client)
	if task == nil {
		return nil
	}
	return task.WaitFor(ctx, proxmoxTaskTimeout)
}
