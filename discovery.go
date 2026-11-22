package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type FoundHost struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	Connected bool   `json:"connected"`
	Message   string `json:"message"`
	// Source is "mdns" for endpoints announced by Android 11+ wireless
	// debugging, "scan" for hosts found by the TCP port probe.
	Source string `json:"source,omitempty"`
}

// localSubnets returns the /24 networks of all active non-loopback IPv4
// interfaces. Equivalent to SkyADB's NetworkInfoProvider + ScanRangeParser.
func localSubnets() []string {
	seen := map[string]bool{}
	out := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			mask := net.CIDRMask(24, 32)
			network := ip.Mask(mask)
			cidr := fmt.Sprintf("%s/24", network.String())
			if !seen[cidr] {
				seen[cidr] = true
				out = append(out, cidr)
			}
		}
	}
	return out
}

// recentSubnets turns remembered device addresses into /24 ranges so the
// default scan also covers subnets that are reachable through a router but are
// not configured on a local interface.
func recentSubnets(list []RecentDevice) string {
	seen := map[string]bool{}
	out := []string{}
	mask := net.CIDRMask(24, 32)
	for _, d := range list {
		ip := net.ParseIP(d.Host).To4()
		if ip == nil {
			continue
		}
		c := (&net.IPNet{IP: ip.Mask(mask), Mask: mask}).String()
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}

// expandCIDR lists usable hosts for a /16 .. /32 network. A /32 (one explicit
// host) is the most common input - people scan the address they already know -
// and used to be rejected as "range too large" because the bounds check was
// inverted.
func expandCIDR(cidr string) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("网段格式有误（示例 192.168.1.0/24 或 192.168.1.5/32）: %s", cidr)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("仅支持 IPv4 网段: %s", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones < 16 || ones > 32 {
		return nil, fmt.Errorf("网段范围需在 /16 ~ /32 之间: %s", cidr)
	}
	if ones == 32 {
		// Single host: keep the address the user typed, not the network base.
		return []string{ip4.String()}, nil
	}
	start := make(net.IP, 4)
	copy(start, ipnet.IP.To4())
	total := 1 << (32 - ones)
	hosts := []string{}
	for n := 0; n < total && len(hosts) < 4096; n++ {
		v := make(net.IP, 4)
		copy(v, start)
		carry := n
		for b := 3; b >= 0; b-- {
			x := int(v[b]) + (carry & 0xff)
			v[b] = byte(x)
			carry = (carry >> 8) + (x >> 8)
		}
		if n == 0 || n == total-1 { // skip network & broadcast
			continue
		}
		hosts = append(hosts, v.String())
	}
	return hosts, nil
}

// AutoConnectAfterPair finds the connect endpoint that belongs to a device we
// just paired with. Android 11+ exposes a pairing port and a *different*
// connect port; without this step the user pairs successfully and then has no
// idea which port to type, which is the single most common "pairing works but
// nothing connects" complaint.
func (a *Adb) AutoConnectAfterPair(host string) string {
	for _, f := range a.mdnsServices(5555) {
		if f.Host == host && f.Connected {
			return net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
		}
	}
	// Fall back to the classic port; harmless when nothing listens.
	if msg, err := a.Connect(host, 5555); err == nil {
		if strings.Contains(msg, "connected") {
			return net.JoinHostPort(host, "5555")
		}
	}
	return ""
}

// mdnsServices asks the adb client for the wireless-debugging endpoints it has
// already seen over mDNS. Android 11+ listens on a random port, so a fixed-port
// TCP scan can never find those devices; pairing/connect ports that adb has
// announced are tried directly.
func (a *Adb) mdnsServices(port int) []FoundHost {
	r := a.run(context.Background(), 8*time.Second, "mdns", "services")
	if r.Err != nil {
		return nil
	}
	found := []FoundHost{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(r.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.Contains(f[1], "_adb-tls-connect._tcp") {
			continue
		}
		host, portStr, err := net.SplitHostPort(f[2])
		if err != nil {
			continue
		}
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 {
			p = port
		}
		key := net.JoinHostPort(host, strconv.Itoa(p))
		if seen[key] {
			continue
		}
		seen[key] = true
		msg, cerr := a.Connect(host, p)
		fh := FoundHost{Host: host, Port: p, Open: true, Connected: cerr == nil, Message: msg, Source: "mdns"}
		if cerr != nil {
			fh.Message = cerr.Error()
		}
		a.diag(map[bool]string{true: "error", false: "info"}[cerr != nil], "mdns", key+" "+fh.Message)
		found = append(found, fh)
	}
	return found
}

// Discover probes the ADB TCP port across the LAN, then issues `adb connect`
// for every responding host. SkyADB uses mDNS + a port probe; on desktop both
// are used: mDNS first (covers Android 11+ random ports), then a bounded scan.
func (a *Adb) Discover(ctx context.Context, ranges string, port int) ([]FoundHost, error) {
	cidrs := []string{}
	if strings.TrimSpace(ranges) != "" {
		for _, part := range strings.FieldsFunc(ranges, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n'
		}) {
			if strings.TrimSpace(part) == "" {
				continue
			}
			cidrs = append(cidrs, part)
		}
	}
	// The local /24 is always worth probing, even when the user typed a range:
	// dropping it silently made discovery find nothing on multi-homed hosts.
	for _, c := range localSubnets() {
		found := false
		for _, have := range cidrs {
			if have == c {
				found = true
				break
			}
		}
		if !found {
			cidrs = append(cidrs, c)
		}
	}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("未发现可用的局域网网段，请在设置中手动指定扫描网段")
	}

	result := a.mdnsServices(port)

	hosts := []string{}
	seen := map[string]bool{}
	for _, c := range cidrs {
		list, err := expandCIDR(c)
		if err != nil {
			if len(result) > 0 {
				continue // mDNS already produced something; skip the bad range
			}
			return nil, err
		}
		for _, h := range list {
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) > 4096 {
		hosts = hosts[:4096]
	}

	st := a.Settings()
	timeout := time.Duration(st.ConnectTimeout) * time.Millisecond
	if timeout < 50*time.Millisecond {
		timeout = 50 * time.Millisecond
	}
	workers := st.ScanConcurrency
	if workers < 1 || workers > 512 {
		workers = 64
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	open := []string{}
dispatch:
	for _, h := range hosts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err == nil {
				conn.Close()
				mu.Lock()
				open = append(open, host)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()

	for _, h := range open {
		dup := false
		for _, f := range result {
			if f.Host == h && f.Port == port {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		msg, err := a.Connect(h, port)
		f := FoundHost{Host: h, Port: port, Open: true, Connected: err == nil, Message: msg, Source: "scan"}
		if err != nil {
			f.Message = err.Error()
		}
		a.diag("info", "discover", fmt.Sprintf("%s:%d %s", h, port, f.Message))
		result = append(result, f)
	}
	a.diag("info", "discover", fmt.Sprintf("扫描 %d 个地址，发现 %d 台设备", len(hosts), len(result)))
	return result, nil
}
