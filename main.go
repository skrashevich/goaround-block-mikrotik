package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	kfile "github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
	routeros "github.com/swoga/go-routeros"
	"github.com/zalando/go-keyring"
)

var (
	Version = "0.0.2"
	k       = koanf.New(".")
)

type RouteInfo struct {
	RouteID    string
	DstAddress string
	Gateway    string
	Comment    string
}

func getConfigFile() (string, error) {
	configDir, err := os.UserConfigDir() // Get the system's default user config directory
	if err != nil {
		fmt.Printf("Error finding user config dir: %s\n", err)
		return "", err
	}

	configPath := filepath.Join(configDir, "go-mikrotik-block")
	os.MkdirAll(configPath, 0700)
	configFile := filepath.Join(configPath, "config.yaml")
	return configFile, nil
}

func initConfig() {
	configFile, _ := getConfigFile()
	fmt.Println("Looking for config in:", configFile)

	if err := k.Load(kfile.Provider(configFile), yaml.Parser()); err != nil {
		fmt.Printf("Error reading config file: %s\n", err)
	} else {
		fmt.Println("Using config file:", configFile)
	}

}

func saveCreds(service, user, password string) error {
	err := keyring.Set(service, user, password)
	if err != nil {
		fmt.Println(fmt.Errorf("%w\n", err))
		return err
	}
	return nil
}

func getCreds(service, user string) (string, error) {
	secret, err := keyring.Get(service, user)
	if err != nil && err.Error() != "secret not found in keyring" {
		fmt.Println(fmt.Errorf("%w\n", err))
		return "", err
	}
	return secret, nil
}

func main() {
	initConfig()
	domain, address, username, password, gateway, listRoutes, doUpdate, dryRun, version, err := parseFlags()
	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(1)
	}
	if version {
		fmt.Println(Version)
		os.Exit(0)
	}

	c, err := connectToRouter(address, username, password)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to connect to RouterOS: %v", err))
	}
	defer c.Close()

	k.Set("gateway", gateway)
	k.Set("address", address)
	k.Set("username", username)
	saveCreds(address, username, password)

	if listRoutes {
		if _, err := listRoutesWithCommentAndGateway(c, gateway, doUpdate, dryRun); err != nil {
			exitWithError(fmt.Sprintf("Failed to list routes: %v", err))
		}

		return // Exit after listing routes
	}

	ips, err := resolveDomain(domain)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to resolve domain %s: %v", domain, err))
	}

	if err := updateRoutes(c, domain, ips, gateway, dryRun); err != nil {
		exitWithError(err.Error())
	}

	if !dryRun {
		fmt.Println("Routes updated successfully.")
	}

	if err := saveConfig(); err != nil {
		exitWithError(err.Error())
	}
}

func saveConfig() error {
	confBytes, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}

	configFile, _ := getConfigFile()

	return os.WriteFile(configFile, confBytes, 0644)
}

func parseFlags() (domain, address, username, password, gateway string, listRoutes bool, doUpdate bool, dryRun bool, version bool, err error) {
	flag.StringVar(&domain, "domain", "", "Domain name to resolve and route")
	flag.StringVar(&address, "address", k.String("address"), "MikroTik RouterOS device address")
	flag.StringVar(&username, "username", k.String("username"), "Username for MikroTik RouterOS")
	flag.StringVar(&password, "password", "", "Password for MikroTik RouterOS")
	flag.StringVar(&gateway, "gateway", k.String("gateway"), "Gateway IP address for the new routes")
	flag.BoolVar(&listRoutes, "list", false, "List existing routes with the specified domain and gateway")
	flag.BoolVar(&doUpdate, "update", false, "Re-resolve existing records and update route records")
	flag.BoolVar(&dryRun, "dry", false, "Simulate the actions without making any changes")
	flag.BoolVar(&version, "version", false, "Print the version of the application and exit")

	flag.Parse()

	if password == "" {
		savedpass, err := getCreds(address, username)
		if err != nil {
			fmt.Printf("Failed to get password from keychain: %v", err)
			return domain, address, username, password, gateway, listRoutes, doUpdate, dryRun, version, fmt.Errorf("Error load credentials from keychain: %v", err)
		}
		password = savedpass
	}

	if doUpdate {
		listRoutes = true
	}

	if ((domain == "" || gateway == "") && !listRoutes) || address == "" || password == "" || username == "" {

		var missingParams []string

		if domain == "" {
			missingParams = append(missingParams, "domain")
		}
		if address == "" {
			missingParams = append(missingParams, "address")
		}
		if username == "" {
			missingParams = append(missingParams, "username")
		}
		if password == "" {
			missingParams = append(missingParams, "password")
		}
		if gateway == "" && !listRoutes {
			missingParams = append(missingParams, "gateway")
		}
		if len(missingParams) > 0 {
			err = fmt.Errorf("Missing required parameters: %s\n", strings.Join(missingParams, ", "))
		}

		// err = fmt.Errorf("Domain, address, username, password, and gateway are required")
		return domain, address, username, password, gateway, listRoutes, doUpdate, dryRun, version, err
	}

	return domain, address, username, password, gateway, listRoutes, doUpdate, dryRun, version, nil
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

	address = net.JoinHostPort(host, port) // Reconstruct address with proper port
	return routeros.Dial(address, username, password)
}

func resolveDomain(domain string) ([]net.IP, error) {
	return net.LookupIP(domain)
}

func updateRoutes(c *routeros.Client, domain string, ips []net.IP, gateway string, dryRun bool) error {
	if err := removeExistingRoutes(c, domain, dryRun); err != nil {
		fmt.Printf("Failed to remove existing routes: %v", err)
	}
	for _, ip := range ips {
		if err := addRoute(c, ip, domain, gateway, dryRun); err != nil {
			fmt.Printf("Failed to add route for IP %s: %v\n", ip.String(), err)
		}
	}
	return nil
}

func removeExistingRoutes(c *routeros.Client, domain string, dryRun bool) error {
	r, err := c.Run("/ip/route/print", "?comment="+domain)
	if err != nil {
		return err
	}

	for _, re := range r.Re {
		cmd := "/ip/route/remove"
		args := "=numbers=" + re.Map[".id"]
		if dryRun {
			fmt.Printf("[removeExistingRoutes]: %s %s\n", cmd, args)
		} else {
			if _, err = c.Run(cmd, args); err != nil {
				fmt.Printf("Failed to remove route: %v\n", err)
			}

			fmt.Println("Remove route: " + cmd + " " + args)
		}
	}
	return nil
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

	args := []string{
		"/ip/route/add",
		"=dst-address=" + ip.String() + "/32",
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
		fmt.Printf("[addRoute] %s\n", args)
	} else {
		_, err = c.RunArgs(args)
		// err := error(nil)

		fmt.Println(strings.Join(args, " "))
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
	if update && !dryRun {
		for i, route := range filteredRoutes {
			resolveAndUpdateRoute(c, &filteredRoutes[i], route.Comment, dryRun)
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

func resolveAndUpdateRoute(c *routeros.Client, route *RouteInfo, domain string, dryRun bool) {
	ips, err := resolveDomain(domain)
	if err != nil {
		fmt.Printf("Failed to resolve domain %s for route ID %s: %v\n", domain, route.RouteID, err)
		return
	}
	updateRoutes(c, domain, ips, route.Gateway, dryRun)
}
