package daemon

import (
	"fmt"
	"net"
	"strings"
)

// AddrsFunc abstracts interface address lookup for unit testing.
type AddrsFunc func(net.Interface) ([]net.Addr, error)

// DiscoverLANIPv4 discovers the primary active non-loopback IPv4 address on the machine.
// It prioritizes standard private network ranges (192.168.x.x > 10.x.x.x > 172.16-31.x.x).
func DiscoverLANIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("enumerating network interfaces: %w", err)
	}

	return selectPrimaryLANIPv4(ifaces, func(ifi net.Interface) ([]net.Addr, error) {
		return ifi.Addrs()
	})
}

// DiscoverAllLANIPv4 returns all active, non-loopback IPv4 addresses found across interfaces.
func DiscoverAllLANIPv4() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerating network interfaces: %w", err)
	}

	return collectAllLANIPv4(ifaces, func(ifi net.Interface) ([]net.Addr, error) {
		return ifi.Addrs()
	})
}

func selectPrimaryLANIPv4(ifaces []net.Interface, addrsFn AddrsFunc) (string, error) {
	all, err := collectAllLANIPv4(ifaces, addrsFn)
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no active LAN IPv4 address discovered")
	}

	// 1. Highest preference: 192.168.0.0/16
	for _, ip := range all {
		if strings.HasPrefix(ip, "192.168.") {
			return ip, nil
		}
	}

	// 2. Second preference: 10.0.0.0/8
	for _, ip := range all {
		if strings.HasPrefix(ip, "10.") {
			return ip, nil
		}
	}

	// 3. Third preference: 172.16.0.0/12 (RFC 1918)
	for _, ip := range all {
		parsed := net.ParseIP(ip)
		if parsed != nil && parsed.IsPrivate() {
			return ip, nil
		}
	}

	// 4. Fallback: first discovered non-loopback IPv4 address
	return all[0], nil
}

func collectAllLANIPv4(ifaces []net.Interface, addrsFn AddrsFunc) ([]string, error) {
	var ips []string

	for _, ifi := range ifaces {
		// Filter out down or loopback interfaces
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := addrsFn(ifi)
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil {
				continue
			}

			ip4 := ip.To4()
			if ip4 == nil {
				continue // Skip IPv6
			}

			// Filter out loopback and link-local (169.254.x.x)
			if ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}

			ips = append(ips, ip4.String())
		}
	}

	return ips, nil
}

// FormatStartupBanner formats the multi-line dashboard banner.
func FormatStartupBanner(host string, port int, lanIP string) string {
	localHost := "localhost"
	if host != "" && host != "0.0.0.0" {
		localHost = host
	}

	var networkEntry string
	if host == "127.0.0.1" || host == "localhost" {
		networkEntry = "disabled (host bound to loopback)"
	} else if lanIP != "" {
		networkEntry = fmt.Sprintf("http://%s:%d", lanIP, port)
	} else {
		networkEntry = "unavailable"
	}

	return fmt.Sprintf("Web Dashboard:\n  Local:   http://%s:%d\n  Network: %s\n  Overlay: http://%s:%d/?mode=overlay",
		localHost, port, networkEntry, localHost, port)
}
