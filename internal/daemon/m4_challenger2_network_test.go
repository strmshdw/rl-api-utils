package daemon

import (
	"errors"
	"net"
	"strings"
	"testing"
)

// mockIPNet creates a net.IPNet pointer for testing.
func mockIPNet(ipStr string, maskBits int) *net.IPNet {
	ip := net.ParseIP(ipStr)
	var mask net.IPMask
	if strings.Contains(ipStr, ":") {
		mask = net.CIDRMask(maskBits, 128)
	} else {
		mask = net.CIDRMask(maskBits, 32)
	}
	return &net.IPNet{IP: ip, Mask: mask}
}

// TestChallenger2_Network_APIPA_And_Loopback_Filtering verifies that loopback (127.x.x.x)
// and APIPA / link-local unicast (169.254.x.x) addresses are strictly filtered out.
func TestChallenger2_Network_APIPA_And_Loopback_Filtering(t *testing.T) {
	ifaces := []net.Interface{
		{
			Index: 1,
			Name:  "lo",
			Flags: net.FlagUp | net.FlagLoopback,
		},
		{
			Index: 2,
			Name:  "eth_apipa",
			Flags: net.FlagUp,
		},
		{
			Index: 3,
			Name:  "eth_down",
			Flags: 0, // FlagUp is NOT set
		},
	}

	addrsMap := map[string][]net.Addr{
		"lo": {
			mockIPNet("127.0.0.1", 8),
			mockIPNet("127.0.0.2", 8),
			mockIPNet("::1", 128),
		},
		"eth_apipa": {
			mockIPNet("169.254.1.10", 16),
			mockIPNet("169.254.254.254", 16),
			mockIPNet("fe80::1", 64),
		},
		"eth_down": {
			mockIPNet("192.168.1.200", 24),
		},
	}

	mockAddrsFn := func(ifi net.Interface) ([]net.Addr, error) {
		return addrsMap[ifi.Name], nil
	}

	// 1. collectAllLANIPv4 must return 0 addresses
	all, err := collectAllLANIPv4(ifaces, mockAddrsFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 discovered IPs, got %v", all)
	}

	// 2. selectPrimaryLANIPv4 must return error indicating no address found
	primary, err := selectPrimaryLANIPv4(ifaces, mockAddrsFn)
	if err == nil {
		t.Errorf("expected error when only loopback and APIPA are present, got: %s", primary)
	}
	if !strings.Contains(err.Error(), "no active LAN IPv4 address discovered") {
		t.Errorf("expected 'no active LAN IPv4 address discovered', got: %v", err)
	}
}

// TestChallenger2_Network_VirtualAndPhysicalNICs tests LAN IP detection across
// multi-interface configurations including WSL, Docker, Hyper-V, physical LAN, and VPNs.
func TestChallenger2_Network_VirtualAndPhysicalNICs(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "vEthernet (WSL)", Flags: net.FlagUp},
		{Index: 3, Name: "vEthernet (Hyper-V)", Flags: net.FlagUp},
		{Index: 4, Name: "docker0", Flags: net.FlagUp},
		{Index: 5, Name: "Ethernet Physical", Flags: net.FlagUp},
		{Index: 6, Name: "Tailscale", Flags: net.FlagUp},
	}

	addrsMap := map[string][]net.Addr{
		"lo": {
			mockIPNet("127.0.0.1", 8),
		},
		"vEthernet (WSL)": {
			mockIPNet("172.28.16.1", 20),
		},
		"vEthernet (Hyper-V)": {
			mockIPNet("172.19.80.1", 20),
		},
		"docker0": {
			mockIPNet("10.255.0.1", 16),
		},
		"Ethernet Physical": {
			mockIPNet("192.168.1.42", 24),
		},
		"Tailscale": {
			mockIPNet("100.100.1.1", 32),
		},
	}

	mockAddrsFn := func(ifi net.Interface) ([]net.Addr, error) {
		return addrsMap[ifi.Name], nil
	}

	// Even though virtual interfaces appear first, 192.168.1.42 must be selected
	primary, err := selectPrimaryLANIPv4(ifaces, mockAddrsFn)
	if err != nil {
		t.Fatalf("unexpected error selecting primary LAN: %v", err)
	}
	if primary != "192.168.1.42" {
		t.Errorf("expected physical '192.168.1.42' to take highest priority, got %q", primary)
	}
}

// TestChallenger2_Network_PreferenceOrder verifies the subnet preference hierarchy:
// 192.168.x.x > 10.x.x.x > 172.16-31.x.x > Fallback
func TestChallenger2_Network_PreferenceOrder(t *testing.T) {
	tests := []struct {
		name     string
		ips      []string
		expected string
	}{
		{
			name:     "192.168 beats 10 and 172",
			ips:      []string{"172.16.0.1", "10.0.0.1", "192.168.0.50"},
			expected: "192.168.0.50",
		},
		{
			name:     "10 beats 172",
			ips:      []string{"172.20.1.1", "10.1.2.3"},
			expected: "10.1.2.3",
		},
		{
			name:     "172 RFC1918 beats public IP",
			ips:      []string{"203.0.113.10", "172.31.0.1"},
			expected: "172.31.0.1",
		},
		{
			name:     "Fallback to first IP when only public or CGNAT present",
			ips:      []string{"100.64.0.1", "203.0.113.5"},
			expected: "100.64.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ifaces []net.Interface
			addrsMap := make(map[string][]net.Addr)

			for i, ipStr := range tc.ips {
				ifName := string(rune('a' + i))
				ifaces = append(ifaces, net.Interface{Index: i + 1, Name: ifName, Flags: net.FlagUp})
				addrsMap[ifName] = []net.Addr{mockIPNet(ipStr, 24)}
			}

			mockAddrsFn := func(ifi net.Interface) ([]net.Addr, error) {
				return addrsMap[ifi.Name], nil
			}

			primary, err := selectPrimaryLANIPv4(ifaces, mockAddrsFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if primary != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, primary)
			}
		})
	}
}

// TestChallenger2_Network_AddrErrorHandling verifies graceful handling if an interface fails addr query.
func TestChallenger2_Network_AddrErrorHandling(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "broken_nic", Flags: net.FlagUp},
		{Index: 2, Name: "working_nic", Flags: net.FlagUp},
	}

	mockAddrsFn := func(ifi net.Interface) ([]net.Addr, error) {
		if ifi.Name == "broken_nic" {
			return nil, errors.New("device I/O error")
		}
		return []net.Addr{mockIPNet("192.168.1.15", 24)}, nil
	}

	primary, err := selectPrimaryLANIPv4(ifaces, mockAddrsFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary != "192.168.1.15" {
		t.Errorf("expected '192.168.1.15', got %q", primary)
	}
}

// TestChallenger2_Network_FormatStartupBanner verifies all permutation scenarios of startup banner.
func TestChallenger2_Network_FormatStartupBanner(t *testing.T) {
	tests := []struct {
		name            string
		host            string
		port            int
		lanIP           string
		expectLocal     string
		expectNetwork   string
		expectOverlay   string
	}{
		{
			name:          "default_0_0_0_0_with_lan",
			host:          "0.0.0.0",
			port:          49125,
			lanIP:         "192.168.1.42",
			expectLocal:   "http://localhost:49125",
			expectNetwork: "http://192.168.1.42:49125",
			expectOverlay: "http://localhost:49125/?mode=overlay",
		},
		{
			name:          "loopback_127_0_0_1_disables_network",
			host:          "127.0.0.1",
			port:          49125,
			lanIP:         "192.168.1.42",
			expectLocal:   "http://127.0.0.1:49125",
			expectNetwork: "disabled (host bound to loopback)",
			expectOverlay: "http://127.0.0.1:49125/?mode=overlay",
		},
		{
			name:          "loopback_localhost_disables_network",
			host:          "localhost",
			port:          49125,
			lanIP:         "192.168.1.42",
			expectLocal:   "http://localhost:49125",
			expectNetwork: "disabled (host bound to loopback)",
			expectOverlay: "http://localhost:49125/?mode=overlay",
		},
		{
			name:          "no_lan_available",
			host:          "0.0.0.0",
			port:          49125,
			lanIP:         "",
			expectLocal:   "http://localhost:49125",
			expectNetwork: "unavailable",
			expectOverlay: "http://localhost:49125/?mode=overlay",
		},
		{
			name:          "custom_port_and_custom_host",
			host:          "192.168.1.100",
			port:          8080,
			lanIP:         "192.168.1.100",
			expectLocal:   "http://192.168.1.100:8080",
			expectNetwork: "http://192.168.1.100:8080",
			expectOverlay: "http://192.168.1.100:8080/?mode=overlay",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			banner := FormatStartupBanner(tc.host, tc.port, tc.lanIP)

			if !strings.Contains(banner, "Local:   "+tc.expectLocal) {
				t.Errorf("banner missing expected local entry %q. Banner:\n%s", tc.expectLocal, banner)
			}
			if !strings.Contains(banner, "Network: "+tc.expectNetwork) {
				t.Errorf("banner missing expected network entry %q. Banner:\n%s", tc.expectNetwork, banner)
			}
			if !strings.Contains(banner, "Overlay: "+tc.expectOverlay) {
				t.Errorf("banner missing expected overlay entry %q. Banner:\n%s", tc.expectOverlay, banner)
			}
		})
	}
}

// TestChallenger2_Network_RealHostDiscovery verifies host discovery does not crash on actual machine.
func TestChallenger2_Network_RealHostDiscovery(t *testing.T) {
	lanIP, err := DiscoverLANIPv4()
	if err == nil {
		if net.ParseIP(lanIP) == nil {
			t.Fatalf("DiscoverLANIPv4 returned invalid IP: %q", lanIP)
		}
		parsed := net.ParseIP(lanIP).To4()
		if parsed == nil {
			t.Fatalf("DiscoverLANIPv4 returned non-IPv4: %q", lanIP)
		}
		if parsed.IsLoopback() {
			t.Errorf("DiscoverLANIPv4 returned loopback IP: %q", lanIP)
		}
		if parsed.IsLinkLocalUnicast() {
			t.Errorf("DiscoverLANIPv4 returned link-local APIPA IP: %q", lanIP)
		}
	}

	allIPs, err := DiscoverAllLANIPv4()
	if err == nil {
		for _, ip := range allIPs {
			parsed := net.ParseIP(ip).To4()
			if parsed == nil {
				t.Errorf("DiscoverAllLANIPv4 returned non-IPv4: %q", ip)
			}
			if parsed.IsLoopback() {
				t.Errorf("DiscoverAllLANIPv4 returned loopback IP: %q", ip)
			}
			if parsed.IsLinkLocalUnicast() {
				t.Errorf("DiscoverAllLANIPv4 returned link-local APIPA IP: %q", ip)
			}
		}
	}
}
