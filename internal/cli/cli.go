package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"loadbalancer/internal/app"
	"loadbalancer/internal/boot"
)

const defaultConfigPath = "configs/app.json"

type Options struct {
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
	Client *http.Client
}

type clusterBootstrapRequest struct {
	NodeID            string                    `json:"node_id"`
	RaftBindAddr      string                    `json:"raft_bind_addr,omitempty"`
	RaftAdvertiseAddr string                    `json:"raft_advertise_addr"`
	RaftTiming        *clusterRaftTimingRequest `json:"raft_timing,omitempty"`
	VIPInterface      string                    `json:"vip_interface,omitempty"`
	VIP               *clusterVIPRequest        `json:"vip,omitempty"`
}

type clusterJoinRequest struct {
	NodeID            string   `json:"node_id"`
	RaftBindAddr      string   `json:"raft_bind_addr,omitempty"`
	RaftAdvertiseAddr string   `json:"raft_advertise_addr"`
	VIPInterface      string   `json:"vip_interface,omitempty"`
	Peers             []string `json:"peers"`
}

type clusterVIPRequest struct {
	Address           string `json:"address"`
	GARPCount         int    `json:"garp_count,omitempty"`
	GARPInterval      string `json:"garp_interval,omitempty"`
	AcquireDelay      string `json:"acquire_delay,omitempty"`
	ReleaseOnShutdown bool   `json:"release_on_shutdown,omitempty"`
}

type clusterRaftTimingRequest struct {
	HeartbeatTimeout   string `json:"heartbeat_timeout,omitempty"`
	ElectionTimeout    string `json:"election_timeout,omitempty"`
	LeaderLeaseTimeout string `json:"leader_lease_timeout,omitempty"`
	CommitTimeout      string `json:"commit_timeout,omitempty"`
}

type stringList []string

func Run(ctx context.Context, opts Options) error {
	opts = normalizeOptions(opts)
	if len(opts.Args) == 0 {
		return runServe(ctx, defaultConfigPath, opts.Stdout)
	}

	switch opts.Args[0] {
	case "serve":
		return runServe(ctx, serveConfigPath(opts.Args[1:]), opts.Stdout)
	case "cluster":
		return runCluster(ctx, opts, opts.Args[1:])
	default:
		return runServe(ctx, opts.Args[0], opts.Stdout)
	}
}

func normalizeOptions(opts Options) Options {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 10 * time.Second}
	}
	return opts
}

func serveConfigPath(args []string) string {
	if len(args) == 0 {
		return defaultConfigPath
	}
	return args[0]
}

func runServe(ctx context.Context, configPath string, stdout io.Writer) error {
	logger := log.New(stdout, "[loadbalancer] ", log.LstdFlags|log.Lmicroseconds)

	cfg, err := boot.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	application, err := app.New(cfg, configPath, logger)
	if err != nil {
		return fmt.Errorf("build app: %w", err)
	}
	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("run app: %w", err)
	}
	return nil
}

func runCluster(ctx context.Context, opts Options, args []string) error {
	if len(args) == 0 {
		return errors.New("cluster command requires status, bootstrap, or join")
	}
	switch args[0] {
	case "status":
		return runClusterStatus(ctx, opts, args[1:])
	case "bootstrap":
		return runClusterBootstrap(ctx, opts, args[1:])
	case "join":
		return runClusterJoin(ctx, opts, args[1:])
	default:
		return fmt.Errorf("unknown cluster command: %s", args[0])
	}
}

func runClusterStatus(ctx context.Context, opts Options, args []string) error {
	fs := newFlagSet("cluster status", opts.Stderr)
	dashboard := fs.String("dashboard", "http://localhost:9090", "dashboard API base URL")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	body, err := doRequest(ctx, opts.Client, http.MethodGet, *dashboard, "/api/node/cluster-status", nil)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(opts.Stdout, strings.TrimRight(string(body), "\n"))
	return err
}

func runClusterBootstrap(ctx context.Context, opts Options, args []string) error {
	fs := newFlagSet("cluster bootstrap", opts.Stderr)
	input := bootstrapFlags(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	request, err := input.request()
	if err != nil {
		return err
	}
	if _, err := doRequest(ctx, opts.Client, http.MethodPost, input.dashboard, "/api/cluster/bootstrap", request); err != nil {
		return err
	}
	_, err = fmt.Fprintln(opts.Stdout, "cluster bootstrapped")
	return err
}

func runClusterJoin(ctx context.Context, opts Options, args []string) error {
	fs := newFlagSet("cluster join", opts.Stderr)
	input := joinFlags(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	request, err := input.request()
	if err != nil {
		return err
	}
	if _, err := doRequest(ctx, opts.Client, http.MethodPost, input.dashboard, "/api/node/join-cluster", request); err != nil {
		return err
	}
	_, err = fmt.Fprintln(opts.Stdout, "node joined cluster")
	return err
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	return nil
}

func doRequest(ctx context.Context, client *http.Client, method, base, path string, body any) ([]byte, error) {
	endpoint, err := endpointURL(base, path)
	if err != nil {
		return nil, err
	}
	payload, err := encodeBody(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return readResponse(client.Do(req))
}

func endpointURL(base, path string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse dashboard URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("dashboard URL must include scheme and host: %s", base)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return bytes.NewReader(payload), nil
}

func readResponse(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, fmt.Errorf("call dashboard API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read dashboard response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("dashboard API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}
