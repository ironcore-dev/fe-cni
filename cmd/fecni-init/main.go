//go:build linux

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ip"
)

const (
	// podCIDRPlaceholder is the template marker replaced with the node's podCIDR.
	podCIDRPlaceholder = "__PODCIDR_SUBNET__"

	// Standard in-cluster config paths mounted by kubelet for every pod.
	//nolint:gosec // G101: This is not a hardcoded credential
	saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// node is the minimal slice of the k8s Node API needed by FeCNI init.
type node struct {
	Spec struct {
		PodCIDR string `json:"podCIDR"`
	} `json:"spec"`
}

func main() {
	nodeName := flag.String("node-name", os.Getenv("NODE_NAME"), "Name of the node to fetch podCIDR for. Defaults to NODE_NAME env var.")
	templatePath := flag.String("template", "/template/fecni.conflist", "Path to the CNI conflist template with __PODCIDR_SUBNET__ placeholder.")
	outputPath := flag.String("output", "/host/etc/cni/net.d/fecni.conflist", "Path to write the rendered CNI conflist.")
	flag.Parse()

	if *nodeName == "" {
		log.Fatalf("node-name is required (set NODE_NAME env var or pass -node-name flag)")
	}

	log.Printf("FeCNI init %s (commit %s, built %s)", Version, GitCommit, BuildTime)
	log.Printf("node: %q", *nodeName)

	podCIDR, err := fetchPodCIDR(*nodeName)
	if err != nil {
		log.Fatalf("failed to fetch podCIDR for node %q: %v", *nodeName, err)
	}
	if podCIDR == "" {
		log.Fatalf("node %q has no spec.podCIDR set. Set it with:\n  kubectl patch node %s --type=merge -p '{\"spec\":{\"podCIDR\":[\"<your-ipv6-subnet>\"]}}'", *nodeName, *nodeName)
	}

	log.Printf("node %q podCIDR: %s", *nodeName, podCIDR)

	if err := renderTemplate(*templatePath, *outputPath, podCIDR); err != nil {
		log.Fatalf("failed to render conflist: %v", err)
	}

	log.Printf("CNI config written to %s", *outputPath)

	if err := ip.EnableIP6Forward(); err != nil {
		log.Fatalf("failed to enable IPv6 forwarding: %v", err)
	}

	log.Println("IPv6 forwarding enabled")
}

// fetchPodCIDR queries the in-cluster kubernetes API for the node's podCIDR.
// It uses the mounted service account token and cluster CA certificate for auth.
func fetchPodCIDR(nodeName string) (string, error) {
	token, err := os.ReadFile(saTokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read service account token: %w", err)
	}

	caCert, err := os.ReadFile(saCAPath)
	if err != nil {
		return "", fmt.Errorf("failed to read cluster CA certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return "", fmt.Errorf("failed to parse cluster CA certificate")
	}

	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return "", fmt.Errorf("KUBERNETES_SERVICE_HOST or KUBERNETES_SERVICE_PORT not set; not running in a pod?")
	}

	// If host is an IPv6 address, it needs to be wrapped in square brackets
	// Checking for presence of ":" is naive but effective way to distinguish
	// raw IPv6 addresses. We can reasonably expect that the KUBERNETES_SERVICE_HOST
	// will be either raw IP address or a hostname.
	if strings.Contains(host, ":") {
		host = fmt.Sprintf("[%s]", host)
	}

	url := &url.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("api/v1/nodes/%s", url.PathEscape(nodeName)),
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}

	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request to %s failed: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected API return code %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var n node
	if err := json.Unmarshal(body, &n); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	return n.Spec.PodCIDR, nil
}

// renderTemplate reads the template, substitutes the podCIDR placeholder, and
// writes the result to the output path.
func renderTemplate(templatePath, outputPath, podCIDR string) error {
	tmpl, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}

	rendered := bytes.ReplaceAll(tmpl, []byte(podCIDRPlaceholder), []byte(podCIDR))

	if err := os.WriteFile(outputPath, rendered, 0o600); err != nil {
		return fmt.Errorf("failed to render CNI config file %s: %w", outputPath, err)
	}

	return nil
}
