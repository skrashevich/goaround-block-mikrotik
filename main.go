package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"

	routeros "github.com/swoga/go-routeros"
)

type RouteInfo struct {
	RouteID    string
	DstAddress string
	Gateway    string
	Comment    string
}

func main() {
	domain, address, username, password, gateway, listRoutes, doUpdate := parseFlags()

	c, err := connectToRouter(address, username, password)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to connect to RouterOS: %v", err))
	}
	defer c.Close()

	if listRoutes {
		if err, _ := listRoutesWithCommentAndGateway(c, gateway, doUpdate); err != nil {
			exitWithError(fmt.Sprintf("Failed to list routes: %v", err))
		}

		return // Exit after listing routes
	}

	ips, err := resolveDomain(domain)
	if err != nil {
		exitWithError(fmt.Sprintf("Failed to resolve domain %s: %v", domain, err))
	}

	if err := updateRoutes(c, domain, ips, gateway); err != nil {
		exitWithError(err.Error())
	}

	fmt.Println("Routes updated successfully.")
}

func parseFlags() (domain, address, username, password, gateway string, listRoutes bool, doUpdate bool) {
	flag.StringVar(&domain, "domain", "", "Domain name to resolve and route")
	flag.StringVar(&address, "address", "", "MikroTik RouterOS device address")
	flag.StringVar(&username, "username", "admin", "Username for MikroTik RouterOS")
	flag.StringVar(&password, "password", "", "Password for MikroTik RouterOS")
	flag.StringVar(&gateway, "gateway", "", "Gateway IP address for the new routes")
	flag.BoolVar(&listRoutes, "list", false, "List existing routes with the specified domain and gateway")
	flag.BoolVar(&doUpdate, "update", false, "Re-resolve existing records and update route records")

	flag.Parse()

	if ((domain == "" || gateway == "") && !listRoutes) || address == "" || password == "" {
		exitWithError("Domain, address, password, and gateway are required")
	}
	return domain, address, username, password, gateway, listRoutes, doUpdate
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

func updateRoutes(c *routeros.Client, domain string, ips []net.IP, gateway string) error {
	if err := removeExistingRoutes(c, domain); err != nil {
		fmt.Printf("Failed to remove existing routes: %v", err)
	}
	for _, ip := range ips {
		if err := addRoute(c, ip, domain, gateway); err != nil {
			fmt.Printf("Failed to add route for IP %s: %v\n", ip.String(), err)
		}
	}
	return nil
}

func removeExistingRoutes(c *routeros.Client, domain string) error {
	r, err := c.Run("/ip/route/print", "?comment="+domain)
	if err != nil {
		return err
	}

	for _, re := range r.Re {
		cmd := "/ip/route/remove"
		args := "=numbers=" + re.Map[".id"]
		if _, err = c.Run(cmd, args); err != nil {
			fmt.Printf("Failed to remove route: %v\n", err)
		}
		fmt.Println("Remove route: " + cmd + " " + args)
	}
	return nil
}

func sanitizeDomain(domain string) string {
	// Replace all instances of "=" with "\="
	safeDomain := strings.ReplaceAll(domain, "=", "\\=")
	// Add more sanitization steps if necessary.
	// For example, you might want to escape other special characters or implement
	// stricter validation rules depending on your use case and security requirements.
	return safeDomain
}

func addRoute(c *routeros.Client, ip net.IP, domain string, gateway string) error {
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

	_, err := c.RunArgs(args)
	// err := error(nil)

	fmt.Println(strings.Join(args, " "))

	return err
}

func exitWithError(msg string) {
	fmt.Println(msg)
	os.Exit(1)
}
func listRoutesWithCommentAndGateway(c *routeros.Client, gateway string, update bool) (error, []RouteInfo) {
	r, err := c.Run("/ip/route/print")
	if err != nil {
		return err, nil
	}

	var routes []RouteInfo

	// Regular expression to match a valid hostname (simplified version)
	hostnameRegex := regexp.MustCompile(`^(?i)[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

	for _, re := range r.Re {
		routeGateway, hasGateway := re.Map["gateway"]
		comment, hasComment := re.Map["comment"]

		// Check if the route matches the given gateway and has a valid hostname as a comment
		if hasGateway && routeGateway == gateway && hasComment && hostnameRegex.MatchString(comment) {
			rinfo := RouteInfo{
				RouteID:    re.Map[".id"],
				DstAddress: re.Map["dst-address"],
				Gateway:    routeGateway,
				Comment:    comment,
			}
			routes = append(routes, rinfo)
			fmt.Printf("Route ID: %s, Dst Address: %s, Gateway: %s, Comment: %s\n",
				rinfo.RouteID, rinfo.DstAddress, rinfo.Gateway, comment)

			if update {
				ip, _ := resolveDomain(rinfo.DstAddress)
				updateRoutes(c, rinfo.Comment, ip, rinfo.Gateway)
			}
		}
	}

	return nil, routes
}
