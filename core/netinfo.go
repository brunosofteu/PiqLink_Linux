package core

import (
	"net"
	"sort"
)

// LocalAddr is an IPv4 address of this device on a local network.
type LocalAddr struct {
	IP       net.IP
	MaskOnes int
	IfName   string
}

// TestLocalAddrs, when set, replaces interface discovery (used by tests).
var TestLocalAddrs []LocalAddr

// preferredIP returns the local address used for the default route, if any.
func preferredIP() net.IP {
	conn, err := net.Dial("udp4", "192.0.2.1:9") // no packet is sent
	if err != nil {
		return nil
	}
	defer conn.Close()
	if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.To4()
	}
	return nil
}

// LocalIPv4s lists usable IPv4 addresses, the default-route one first.
func LocalIPv4s() []LocalAddr {
	if TestLocalAddrs != nil {
		return TestLocalAddrs
	}
	var out []LocalAddr
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			ones, _ := ipn.Mask.Size()
			out = append(out, LocalAddr{IP: ip4, MaskOnes: ones, IfName: ifc.Name})
		}
	}
	pref := preferredIP()
	sort.SliceStable(out, func(i, j int) bool {
		pi := pref != nil && out[i].IP.Equal(pref)
		pj := pref != nil && out[j].IP.Equal(pref)
		if pi != pj {
			return pi
		}
		return out[i].IP.IsPrivate() && !out[j].IP.IsPrivate()
	})
	return out
}

// IsPublicIPv4 reports whether ip is a globally routable IPv4 address
// (not private, not carrier-grade NAT 100.64.0.0/10, not loopback).
func IsPublicIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil || ip4.IsPrivate() || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
		return false
	}
	if ip4[0] == 100 && ip4[1]&0xC0 == 64 {
		return false
	}
	return true
}
