package machine

import (
	"context"
	"crypto/sha256"
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

	"github.com/codingsince1985/checksum"
	proxmoxapi "github.com/luthermonson/go-proxmox"
	"github.com/spectrocloud/peg/pkg/machine/internal/utils"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// --- ISO transfer helpers ---

const (
	// Timeout for ISO download tasks (generous for multi-GB ISOs)
	isoTransferTimeout = 600 // seconds
)

// isProxmoxStorageRef returns true if the ISO string is a Proxmox storage
// reference like "local:iso/foo.iso" or "ceph:iso/bar.iso".
func isProxmoxStorageRef(iso string) bool {
	parts := strings.SplitN(iso, ":", 2)
	if len(parts) != 2 {
		return false
	}
	return strings.HasPrefix(parts[1], "iso/")
}

// prepareISO handles ISO provisioning for the Proxmox backend.
// It detects the ISO source type and ensures the ISO is available on Proxmox storage.
func (p *Proxmox) prepareISO(ctx context.Context, cfg *types.ProxmoxConfig) error {
	iso := p.machineConfig.ISO
	if iso == "" {
		return nil
	}

	// Already a Proxmox storage reference — use as-is
	if isProxmoxStorageRef(iso) {
		log.Infof("ISO is a Proxmox storage reference, using as-is: %s", iso)
		return nil
	}

	isoStorage := cfg.ISOStorage
	if isoStorage == "" {
		isoStorage = "local"
	}

	// HTTP/HTTPS URL — tell Proxmox to download directly
	if utils.IsValidURL(iso) {
		log.Infof("ISO is a URL, directing Proxmox to download: %s", iso)
		return p.downloadURLToProxmox(ctx, cfg, iso, isoStorage)
	}

	// Local file path — serve via temp HTTP server and use StorageDownloadURL
	log.Infof("ISO is a local file, transferring to Proxmox: %s", iso)
	return p.transferLocalISO(ctx, cfg, iso, isoStorage)
}

// downloadURLToProxmox tells Proxmox to download an ISO directly from a URL.
func (p *Proxmox) downloadURLToProxmox(ctx context.Context, cfg *types.ProxmoxConfig, isoURL, isoStorage string) error {
	parsedURL, err := url.Parse(isoURL)
	if err != nil {
		return fmt.Errorf("invalid ISO URL %q: %w", isoURL, err)
	}
	filename := proxmoxURLISOName(isoURL, parsedURL.Path)

	opts := &proxmoxapi.StorageDownloadURLOptions{
		Content:  "iso",
		Filename: filename,
		Storage:  isoStorage,
		URL:      isoURL,
		Node:     cfg.Node,
	}

	// Use checksum if provided
	if p.machineConfig.ISOChecksum != "" {
		alg, hash, err := parseChecksum(p.machineConfig.ISOChecksum)
		if err != nil {
			return err
		}
		opts.Checksum = hash
		opts.ChecksumAlgorithm = alg
	}

	upid, err := p.node.StorageDownloadURL(ctx, opts)
	if err != nil {
		return fmt.Errorf("StorageDownloadURL failed: %w", err)
	}

	task := proxmoxapi.NewTask(proxmoxapi.UPID(upid), p.client)
	if err := task.WaitFor(ctx, isoTransferTimeout); err != nil {
		return fmt.Errorf("ISO download task failed: %w", err)
	}

	p.machineConfig.ISO = fmt.Sprintf("%s:iso/%s", isoStorage, filename)
	log.Infof("ISO available on Proxmox storage: %s", p.machineConfig.ISO)
	return nil
}

// transferLocalISO serves a local ISO file via a temporary HTTP server and
// directs Proxmox to download it via StorageDownloadURL.
func (p *Proxmox) transferLocalISO(ctx context.Context, cfg *types.ProxmoxConfig, isoPath, isoStorage string) error {
	// Validate the local file exists
	fi, err := os.Stat(isoPath)
	if err != nil {
		return fmt.Errorf("ISO file not found: %w", err)
	}

	log.Infof("Computing SHA256 checksum for %s...", isoPath)
	sha256sum, err := checksum.SHA256sum(isoPath)
	if err != nil {
		return fmt.Errorf("failed to compute SHA256: %w", err)
	}
	filename := fmt.Sprintf("peg-%s-%s", sha256sum[:12], sanitizeProxmoxName(filepath.Base(isoPath)))

	// Check if ISO already exists on Proxmox with matching size
	exists, err := p.isoExistsOnStorage(ctx, isoStorage, filename, fi.Size())
	if err != nil {
		return fmt.Errorf("failed to check existing ISO: %w", err)
	}
	if exists {
		log.Infof("ISO %q already current on Proxmox storage %q, skipping upload", filename, isoStorage)
		p.machineConfig.ISO = fmt.Sprintf("%s:iso/%s", isoStorage, filename)
		return nil
	}

	// Detect the local IP that can reach the Proxmox host
	apiEndpoint, err := proxmoxAPIEndpoint(cfg.APIURL)
	if err != nil {
		return err
	}
	localIP, err := detectLocalIP(apiEndpoint)
	if err != nil {
		return err
	}
	log.Infof("Detected local IP reachable from Proxmox: %s", localIP)

	// Start temporary HTTP server
	serveURL, shutdown, err := serveISO(isoPath, localIP)
	if err != nil {
		return fmt.Errorf("failed to start temp HTTP server: %w", err)
	}
	defer func() {
		if err := shutdown(); err != nil {
			log.Warnf("Failed to stop temporary ISO server: %v", err)
		}
	}()
	log.Infof("SHA256: %s", sha256sum)

	// Tell Proxmox to download from our temp HTTP server
	opts := &proxmoxapi.StorageDownloadURLOptions{
		Content:           "iso",
		Filename:          filename,
		Storage:           isoStorage,
		URL:               serveURL,
		Checksum:          sha256sum,
		ChecksumAlgorithm: "sha256",
		Node:              cfg.Node,
	}

	upid, err := p.node.StorageDownloadURL(ctx, opts)
	if err != nil {
		return fmt.Errorf("StorageDownloadURL failed: %w", err)
	}

	log.Infof("Proxmox downloading ISO from %s (task: %s)", serveURL, upid)
	task := proxmoxapi.NewTask(proxmoxapi.UPID(upid), p.client)
	if err := task.WaitFor(ctx, isoTransferTimeout); err != nil {
		return fmt.Errorf("ISO download task failed: %w", err)
	}

	p.machineConfig.ISO = fmt.Sprintf("%s:iso/%s", isoStorage, filename)
	log.Infof("ISO transferred to Proxmox storage: %s", p.machineConfig.ISO)
	return nil
}

// isoExistsOnStorage checks whether a Peg-owned, content-addressed ISO already
// exists. A size mismatch is reported rather than deleting shared storage.
func (p *Proxmox) isoExistsOnStorage(ctx context.Context, storageName, filename string, localSize int64) (bool, error) {
	storage, err := p.node.Storage(ctx, storageName)
	if err != nil {
		return false, fmt.Errorf("failed to get storage %q: %w", storageName, err)
	}

	contents, err := storage.GetContent(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list storage %q: %w", storageName, err)
	}
	return pegISOExists(contents, storageName, filename, localSize)
}

func pegISOExists(contents []*proxmoxapi.StorageContent, storageName, filename string, localSize int64) (bool, error) {
	wantedVolume := fmt.Sprintf("%s:iso/%s", storageName, filename)
	for _, content := range contents {
		if content == nil || content.Volid != wantedVolume {
			continue
		}
		remoteSize := int64(content.Size)
		if remoteSize == localSize {
			return true, nil
		}
		return false, fmt.Errorf("ISO %q exists with unexpected size (local: %d, remote: %d); refusing to delete it", filename, localSize, remoteSize)
	}
	return false, nil
}

// detectLocalIP finds the local IP address that routes to the Proxmox API.
func detectLocalIP(apiEndpoint string) (string, error) {
	conn, err := net.DialTimeout("tcp", apiEndpoint, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to detect local IP reachable from Proxmox API %s: %w", apiEndpoint, err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.TCPAddr)
	return localAddr.IP.String(), nil
}

// serveISO starts a temporary HTTP server that serves a single ISO file.
// It returns the full URL to the file and a shutdown function.
func serveISO(filePath, bindIP string) (url string, shutdown func() error, err error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(bindIP, "0"))
	if err != nil {
		return "", nil, fmt.Errorf("failed to bind temp HTTP server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/iso", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		log.Infof("Serving ISO to %s", r.RemoteAddr)
		http.ServeFile(w, r, filePath)
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Temporary ISO server failed: %v", err)
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	url = "http://" + net.JoinHostPort(addr.IP.String(), strconv.Itoa(addr.Port)) + "/iso"
	shutdown = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}

	log.Infof("Temp HTTP server listening at %s", url)
	return url, shutdown, nil
}

// parseChecksum splits a checksum string like "sha256:abc123" into algorithm and hash.
// If no algorithm prefix is present, defaults to "sha256".
func parseChecksum(cs string) (alg, hash string, err error) {
	parts := strings.SplitN(cs, ":", 2)
	if len(parts) == 2 {
		alg, hash = strings.ToLower(parts[0]), parts[1]
	} else {
		alg, hash = "sha256", cs
	}
	if hash == "" {
		return "", "", fmt.Errorf("ISO checksum hash is empty")
	}
	switch alg {
	case "md5", "sha1", "sha224", "sha256", "sha384", "sha512":
		return alg, hash, nil
	default:
		return "", "", fmt.Errorf("unsupported Proxmox ISO checksum algorithm %q", alg)
	}
}

func proxmoxURLISOName(isoURL, urlPath string) string {
	urlHash := sha256.Sum256([]byte(isoURL))
	return fmt.Sprintf("peg-%x-%s", urlHash[:6], sanitizeProxmoxName(filepath.Base(urlPath)))
}

func sanitizeProxmoxName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "image.iso"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
}
