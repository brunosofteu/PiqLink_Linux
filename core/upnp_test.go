package core

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fakeDesc = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><device>
<deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
<serviceList><service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
<controlURL>/ctl/IPConn</controlURL></service></serviceList>
</device></deviceList></device></deviceList></device></root>`

func TestUPnPFakeRouter(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/desc.xml" {
			io.WriteString(w, fakeDesc)
			return
		}
		body, _ := io.ReadAll(r.Body)
		action := r.Header.Get("SOAPAction")
		calls = append(calls, action)
		switch {
		case strings.Contains(action, "GetExternalIPAddress"):
			io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"><NewExternalIPAddress>85.4.200.17</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`)
		case strings.Contains(action, "AddPortMapping"):
			if strings.Contains(string(body), "<NewExternalPort>47770<") {
				w.WriteHeader(500) // port in use
				io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>718</errorCode><errorDescription>ConflictInMappingEntry</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
				return
			}
			if strings.Contains(string(body), "<NewLeaseDuration>900<") {
				w.WriteHeader(500)
				io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>725</errorCode><errorDescription>OnlyPermanentLeasesSupported</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
				return
			}
			io.WriteString(w, `<ok/>`)
		default:
			io.WriteString(w, `<ok/>`)
		}
	}))
	defer srv.Close()
	gw, err := gatewayFromDescription(srv.URL + "/desc.xml")
	if err != nil {
		t.Fatal(err)
	}
	gw.localIP = net.IPv4(192, 168, 1, 23)
	if !strings.HasSuffix(gw.controlURL, "/ctl/IPConn") {
		t.Fatal(gw.controlURL)
	}
	ip, err := gw.ExternalIP()
	if err != nil || ip.String() != "85.4.200.17" || !IsPublicIPv4(ip) {
		t.Fatal(ip, err)
	}
	m, err := gw.AddMapping(47770)
	if err != nil || m.ExternalPort != 47771 {
		t.Fatal(m, err)
	}
	m.Remove()
	if !strings.Contains(calls[len(calls)-1], "DeletePortMapping") {
		t.Fatal(calls)
	}
	if IsPublicIPv4(net.IPv4(100, 72, 3, 4)) || IsPublicIPv4(net.IPv4(192, 168, 0, 1)) {
		t.Fatal("CGNAT/private detected as public")
	}
}
