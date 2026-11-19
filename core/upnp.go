package core

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Minimal UPnP IGD client: discovers the router, reads the public address
// and opens / closes a TCP port mapping.

type Gateway struct {
	controlURL  string
	serviceType string
	localIP     net.IP
}

type PortMapping struct {
	gw           *Gateway
	ExternalPort int
	ExternalIP   net.IP
}

var httpc = &http.Client{Timeout: 6 * time.Second}

// DiscoverGateway looks for a UPnP Internet Gateway on the local network.
func DiscoverGateway(localIP net.IP, timeout time.Duration) (*Gateway, error) {
	laddr := &net.UDPAddr{IP: localIP}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		conn, err = net.ListenUDP("udp4", nil)
		if err != nil {
			return nil, err
		}
	}
	defer conn.Close()
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	targets := []string{
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
		"urn:schemas-upnp-org:device:InternetGatewayDevice:2",
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANIPConnection:2",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
	}
	send := func() {
		for _, st := range targets {
			msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + st + "\r\n\r\n"
			conn.WriteTo([]byte(msg), dst)
		}
	}
	send()
	deadline := time.Now().Add(timeout)
	resend := time.Now().Add(timeout / 3)
	seen := map[string]bool{}
	buf := make([]byte, 2048)
	for time.Now().Before(deadline) {
		if time.Now().After(resend) {
			send()
			resend = deadline
		}
		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:n])), nil)
		if err != nil {
			continue
		}
		loc := resp.Header.Get("Location")
		if loc == "" || seen[loc] {
			continue
		}
		seen[loc] = true
		gw, err := gatewayFromDescription(loc)
		if err == nil {
			gw.localIP = localIP
			return gw, nil
		}
	}
	return nil, errors.New(Tr("nessun router UPnP trovato (UPnP disattivato sul router?)", "no UPnP router found (UPnP disabled on the router?)"))
}

type xmlDevice struct {
	Services []struct {
		ServiceType string `xml:"serviceType"`
		ControlURL  string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Devices []xmlDevice `xml:"deviceList>device"`
}

type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

func gatewayFromDescription(loc string) (*Gateway, error) {
	resp, err := httpc.Get(loc)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var root xmlRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return nil, err
	}
	base, _ := url.Parse(loc)
	if root.URLBase != "" {
		if b, err := url.Parse(root.URLBase); err == nil {
			base = b
		}
	}
	var find func(d xmlDevice) *Gateway
	find = func(d xmlDevice) *Gateway {
		for _, s := range d.Services {
			if strings.Contains(s.ServiceType, "WANIPConnection") || strings.Contains(s.ServiceType, "WANPPPConnection") {
				u, err := base.Parse(strings.TrimSpace(s.ControlURL))
				if err == nil {
					return &Gateway{controlURL: u.String(), serviceType: strings.TrimSpace(s.ServiceType)}
				}
			}
		}
		for _, sub := range d.Devices {
			if g := find(sub); g != nil {
				return g
			}
		}
		return nil
	}
	if g := find(root.Device); g != nil {
		return g, nil
	}
	return nil, errors.New(Tr("il router non offre il servizio WANIPConnection", "the router does not offer the WANIPConnection service"))
}

type soapFault struct {
	Code        int    `xml:"Body>Fault>detail>UPnPError>errorCode"`
	Description string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
}

func (g *Gateway) soap(action string, args [][2]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, g.serviceType)
	for _, a := range args {
		var v bytes.Buffer
		xml.EscapeText(&v, []byte(a[1]))
		fmt.Fprintf(&b, "<%s>%s</%s>", a[0], v.String(), a[0])
	}
	fmt.Fprintf(&b, `</u:%s></s:Body></s:Envelope>`, action)
	req, _ := http.NewRequest("POST", g.controlURL, strings.NewReader(b.String()))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", fmt.Sprintf(`"%s#%s"`, g.serviceType, action))
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		var f soapFault
		xml.Unmarshal(body, &f)
		return nil, &upnpError{code: f.Code, desc: f.Description, status: resp.StatusCode}
	}
	return body, nil
}

type upnpError struct {
	code   int
	desc   string
	status int
}

func (e *upnpError) Error() string {
	if e.code != 0 {
		return fmt.Sprintf(Tr("errore UPnP %d %s", "UPnP error %d %s"), e.code, e.desc)
	}
	return fmt.Sprintf(Tr("errore UPnP (HTTP %d)", "UPnP error (HTTP %d)"), e.status)
}

// ExternalIP asks the router for its public address.
func (g *Gateway) ExternalIP() (net.IP, error) {
	body, err := g.soap("GetExternalIPAddress", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		IP string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
	}
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(r.IP))
	if ip == nil {
		return nil, errors.New(Tr("il router non conosce il proprio indirizzo pubblico", "the router does not know its public address"))
	}
	return ip, nil
}

// AddMapping opens one of the external ports NETBasePort..+15 towards internalPort.
func (g *Gateway) AddMapping(internalPort int) (*PortMapping, error) {
	var last error
	for i := 0; i < 16; i++ {
		ext := NETBasePort + i
		for _, lease := range []string{"900", "0"} {
			_, err := g.soap("AddPortMapping", [][2]string{
				{"NewRemoteHost", ""},
				{"NewExternalPort", fmt.Sprint(ext)},
				{"NewProtocol", "TCP"},
				{"NewInternalPort", fmt.Sprint(internalPort)},
				{"NewInternalClient", g.localIP.String()},
				{"NewEnabled", "1"},
				{"NewPortMappingDescription", "PiqLink"},
				{"NewLeaseDuration", lease},
			})
			if err == nil {
				return &PortMapping{gw: g, ExternalPort: ext}, nil
			}
			last = err
			if ue, ok := err.(*upnpError); ok && ue.code == 725 {
				continue // only permanent leases supported: retry with 0
			}
			break
		}
	}
	return nil, fmt.Errorf(Tr("il router non ha aperto la porta: %v", "the router did not open the port: %v"), last)
}

// Remove deletes the port mapping.
func (m *PortMapping) Remove() {
	if m == nil || m.gw == nil {
		return
	}
	m.gw.soap("DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", fmt.Sprint(m.ExternalPort)},
		{"NewProtocol", "TCP"},
	})
}
