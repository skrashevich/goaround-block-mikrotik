package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	kfile "github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
	routeros "github.com/swoga/go-routeros"
	"github.com/zalando/go-keyring"
)

var (
	Version = "0.0.2"
)

type Config struct {
	Domain     string
	Address    string
	Username   string
	Password   string
	Gateway    string
	ListRoutes bool
	DoUpdate   bool
	DryRun     bool
	Version    bool
}

type RouteInfo struct {
	RouteID    string
	DstAddress string
	Gateway    string
	Comment    string
}

func getConfigFile() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding user config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "go-mikrotik-block")
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	configFile := filepath.Join(configPath, "config.yaml")
	return configFile, nil
}

func initConfig(k *koanf.Koanf) {
	configFile, err := getConfigFile()
	if err != nil {
		slog.Warn("Config file lookup failed", "error", err)
		return
	}
	slog.Info("Looking for config", "path", configFile)

	if err := k.Load(kfile.Provider(configFile), yaml.Parser()); err != nil {
		slog.Warn("Error reading config file", "error", err)
	} else {
		slog.Info("Using config file", "path", configFile)
	}
}

func saveCreds(service, user, password string) error {
	err := keyring.Set(service, user, password)
	if err != nil {
		slog.Error("Error saving credentials", "error", err)
		return err
	}
	return nil
}

func getCreds(service, user string) (string, error) {
	secret, err := keyring.Get(service, user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		slog.Error("Error retrieving credentials", "error", err)
		return "", err
	}
	return secret, nil
}

func main() {
	k := koanf.New(".")
	initConfig(k)
	cfg, err := parseFlags(k)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if cfg.Version {
		fmt.Println(Version)
		os.Exit(0)
	}

	c, err := connectToRouter(cfg.Address, cfg.Username, cfg.Password)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to connect to RouterOS: %v", err))
	}
	defer c.Close()

	// Password is stored in keyring via saveCreds, never in config file
	k.Set("gateway", cfg.Gateway)
	k.Set("address", cfg.Address)
	k.Set("username", cfg.Username)
	if err := saveCreds(cfg.Address, cfg.Username, cfg.Password); err != nil {
		slog.Warn("Failed to save credentials", "error", err)
	}

	if cfg.ListRoutes {
		if _, err := listRoutesWithCommentAndGateway(c, cfg.Gateway, cfg.DoUpdate, cfg.DryRun); err != nil {
			exitWithError(fmt.Sprintf("Failed to list routes: %v", err))
		}

		return // Exit after listing routes
	}

	ips, err := resolveDomain(cfg.Domain)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to resolve domain %s: %v", cfg.Domain, err))
	}

	if err := updateRoutes(c, cfg.Domain, ips, cfg.Gateway, cfg.DryRun); err != nil {
		exitWithError(err.Error())
	}

	if !cfg.DryRun {
		fmt.Println("Routes updated successfully.")
	}

	if err := saveConfig(k); err != nil {
		exitWithError(err.Error())
	}
}

func saveConfig(k *koanf.Koanf) error {
	confBytes, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}

	configFile, err := getConfigFile()
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, confBytes, 0600)
}

func parseFlags(k *koanf.Koanf) (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Domain, "domain", "", "Domain name to resolve and route")
	flag.StringVar(&cfg.Address, "address", k.String("address"), "MikroTik RouterOS device address")
	flag.StringVar(&cfg.Username, "username", k.String("username"), "Username for MikroTik RouterOS")
	flag.StringVar(&cfg.Password, "password", "", "Password for MikroTik RouterOS")
	flag.StringVar(&cfg.Gateway, "gateway", k.String("gateway"), "Gateway IP address for the new routes")
	flag.BoolVar(&cfg.ListRoutes, "list", false, "List existing routes with the specified domain and gateway")
	flag.BoolVar(&cfg.DoUpdate, "update", false, "Re-resolve existing records and update route records")
	flag.BoolVar(&cfg.DryRun, "dry", false, "Simulate the actions without making any changes")
	flag.BoolVar(&cfg.Version, "version", false, "Print the version of the application and exit")

	flag.Parse()

	if cfg.Version {
		return cfg, nil
	}

	if cfg.Password == "" {
		savedpass, err := getCreds(cfg.Address, cfg.Username)
		if err != nil {
			return cfg, fmt.Errorf("error loading credentials from keychain: %v", err)
		}
		cfg.Password = savedpass
	}

	if cfg.DoUpdate {
		cfg.ListRoutes = true
	}

	if ((cfg.Domain == "" || cfg.Gateway == "") && !cfg.ListRoutes) || cfg.Address == "" || cfg.Password == "" || cfg.Username == "" {

		var missingParams []string

		if cfg.Domain == "" {
			missingParams = append(missingParams, "domain")
		}
		if cfg.Address == "" {
			missingParams = append(missingParams, "address")
		}
		if cfg.Username == "" {
			missingParams = append(missingParams, "username")
		}
		if cfg.Password == "" {
			missingParams = append(missingParams, "password")
		}
		if cfg.Gateway == "" && !cfg.ListRoutes {
			missingParams = append(missingParams, "gateway")
		}
		if len(missingParams) > 0 {
			return cfg, fmt.Errorf("Missing required parameters: %s\n", strings.Join(missingParams, ", "))
		}

		return cfg, nil
	}

	return cfg, nil
}

const defaultRouterOSPort = "8728"

func connectToRouter(address, username, password string) (*routeros.Client, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if addrError, ok := err.(*net.AddrError); ok && addrError.Err == "missing port in address" {
			port = defaultRouterOSPort // Assign default port if missing
			host = address
		} else {
			return nil, err // Return the original error if it's not a missing port error
		}
	}

	if port == "" {
		port = defaultRouterOSPort // Ensure port is set
	}

	address = net.JoinHostPort(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return routeros.DialContext(ctx, address, username, password, 10*time.Second)
}

// Public DNS resolvers from different providers and regions.
// Using multiple resolvers increases the chance of discovering all IPs
// served by geo-distributed CDNs and DNS-based load balancers.
var dnsResolvers = []string{
	// Google
	"8.8.8.8:53",
	"8.8.4.4:53",
	// Cloudflare
	"1.1.1.1:53",
	"1.0.0.1:53",
	// Quad9
	"9.9.9.9:53",
	"149.112.112.112:53",
	// OpenDNS (Cisco)
	"208.67.222.222:53",
	"208.67.220.220:53",
	// Yandex
	"77.88.8.8:53",
	"77.88.8.1:53",
	// Comodo Secure DNS
	"8.26.56.26:53",
	"8.20.247.20:53",
	// Level3 / CenturyLink
	"4.2.2.1:53",
	"4.2.2.2:53",
	// AdGuard
	"94.140.14.14:53",
	"94.140.15.15:53",
	// CleanBrowsing
	"185.228.168.9:53",
	"185.228.169.9:53",
	// Neustar / UltraDNS
	"64.6.64.6:53",
	"64.6.65.6:53",
}

func resolveDomain(domain string) ([]net.IP, error) {
	type result struct {
		ips []net.IP
		src string
		err error
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := make(chan result, len(dnsResolvers)+1)
	var wg sync.WaitGroup

	// System resolver (uses /etc/resolv.conf, OS cache, etc.)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", domain)
		ch <- result{ips: ips, src: "system", err: err}
	}()

	// All public resolvers in parallel
	for _, server := range dnsResolvers {
		wg.Add(1)
		go func(srv string) {
			defer wg.Done()
			resolver := &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					d := net.Dialer{Timeout: 3 * time.Second}
					return d.DialContext(ctx, "udp", srv)
				},
			}
			ips, err := resolver.LookupIP(ctx, "ip", domain)
			ch <- result{ips: ips, src: srv, err: err}
		}(server)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	seen := make(map[string]struct{})
	var allIPs []net.IP
	var successCount int

	for r := range ch {
		if r.err != nil {
			slog.Debug("DNS resolver failed", "resolver", r.src, "error", r.err)
			continue
		}
		successCount++
		for _, ip := range r.ips {
			key := ip.String()
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				allIPs = append(allIPs, ip)
			}
		}
	}

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("failed to resolve %s: no IPs from %d resolvers", domain, len(dnsResolvers)+1)
	}

	slog.Info("Domain resolved",
		"domain", domain,
		"unique_ips", len(allIPs),
		"resolvers_succeeded", successCount,
		"resolvers_total", len(dnsResolvers)+1,
	)

	return allIPs, nil
}

func updateRoutes(c *routeros.Client, domain string, ips []net.IP, gateway string, dryRun bool) error {
	var errs []error
	if err := removeExistingRoutes(c, domain, dryRun); err != nil {
		errs = append(errs, fmt.Errorf("removing existing routes: %w", err))
	}
	for _, ip := range ips {
		if err := addRoute(c, ip, domain, gateway, dryRun); err != nil {
			errs = append(errs, fmt.Errorf("adding route for IP %s: %w", ip.String(), err))
		}
	}
	return errors.Join(errs...)
}

func removeExistingRoutes(c *routeros.Client, domain string, dryRun bool) error {
	safeDomain := sanitizeDomain(domain)
	r, err := c.Run("/ip/route/print", "?comment="+safeDomain)
	if err != nil {
		return err
	}

	var errs []error
	for _, re := range r.Re {
		cmd := "/ip/route/remove"
		args := "=numbers=" + re.Map[".id"]
		if dryRun {
			slog.Info("Dry run: remove route", "cmd", cmd, "args", args)
		} else {
			if _, err = c.Run(cmd, args); err != nil {
				slog.Error("Failed to remove route", "error", err)
				errs = append(errs, fmt.Errorf("removing route %s: %w", re.Map[".id"], err))
			} else {
				slog.Info("Remove route", "cmd", cmd, "args", args)
			}
		}
	}
	return errors.Join(errs...)
}

func sanitizeDomain(domain string) string {
	// Map of characters to be replaced: key is the target, value is the replacement.
	replacements := map[string]string{
		"=": "\\=",
		// Add more replacements as needed. For example:
		// "&": "\\&",
		// "?": "\\?",
	}

	safeDomain := domain
	for target, replacement := range replacements {
		safeDomain = strings.ReplaceAll(safeDomain, target, replacement)
	}

	return safeDomain
}

func addRoute(c *routeros.Client, ip net.IP, domain string, gateway string, dryRun bool) error {
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}
	if gateway == "" {
		return fmt.Errorf("gateway is required")
	}

	// Sanitize the domain to prevent command injection.
	safeDomain := sanitizeDomain(domain)

	prefix := "/32"
	if ip.To4() == nil {
		prefix = "/128"
	}

	args := []string{
		"/ip/route/add",
		"=dst-address=" + ip.String() + prefix,
		"=gateway=" + gateway,
		"=comment=" + safeDomain,
	}

	// Check if the gateway is a valid IP address
	if net.ParseIP(gateway) != nil {
		// It's valid, add the check-gateway line
		args = append(args, "=check-gateway=arp")
	}
	var err error
	if dryRun {
		slog.Info("Dry run: add route", "args", args)
	} else {
		_, err = c.RunArgs(args)
		slog.Info("Add route", "args", strings.Join(args, " "))
	}

	return err
}

func exitWithError(msg string) {
	fmt.Println(msg)
	os.Exit(1)
}

var hostnameRegex = regexp.MustCompile(`^(?i)[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

func listRoutesWithCommentAndGateway(c *routeros.Client, gateway string, update bool, dryRun bool) ([]RouteInfo, error) {
	routes, err := fetchRoutes(c)
	if err != nil {
		return nil, err
	}

	filteredRoutes := filterRoutesByGatewayAndComment(routes, gateway)
	if update {
		var errs []error
		for i, route := range filteredRoutes {
			if err := resolveAndUpdateRoute(c, &filteredRoutes[i], route.Comment, dryRun); err != nil {
				errs = append(errs, err)
			}
		}
		if err := errors.Join(errs...); err != nil {
			return filteredRoutes, err
		}
	}

	return filteredRoutes, nil
}

func fetchRoutes(c *routeros.Client) ([]RouteInfo, error) {
	r, err := c.Run("/ip/route/print")
	if err != nil {
		return nil, err
	}

	var routes []RouteInfo
	for _, re := range r.Re {
		routes = append(routes, RouteInfo{
			RouteID:    re.Map[".id"],
			DstAddress: re.Map["dst-address"],
			Gateway:    re.Map["gateway"],
			Comment:    re.Map["comment"],
		})
	}
	return routes, nil
}

func filterRoutesByGatewayAndComment(routes []RouteInfo, gateway string) []RouteInfo {
	var filteredRoutes []RouteInfo
	for _, route := range routes {
		if route.Gateway == gateway && hostnameRegex.MatchString(route.Comment) {
			fmt.Printf("Route ID: %s, Dst Address: %s, Gateway: %s, Comment: %s\n",
				route.RouteID, route.DstAddress, route.Gateway, route.Comment)
			filteredRoutes = append(filteredRoutes, route)
		}
	}
	return filteredRoutes
}

func resolveAndUpdateRoute(c *routeros.Client, route *RouteInfo, domain string, dryRun bool) error {
	ips, err := resolveDomain(domain)
	if err != nil {
		return fmt.Errorf("resolving domain %s for route ID %s: %w", domain, route.RouteID, err)
	}
	return updateRoutes(c, domain, ips, route.Gateway, dryRun)
}
