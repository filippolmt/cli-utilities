// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"fritzbox-pp-cli/internal/fritzbox"
)

// fakeBox serves the web data path and TR-064 on one port, the way FRITZ!OS 8
// answers them: the landevice medium fields empty, the medium in the TR-064
// host list. tr064OK=false makes the Hosts1 action fail with a SOAP fault.
func fakeBox(t *testing.T, tr064OK bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/query.lua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hosts":[
			{"name":"pfSense","ip":"192.168.189.2","mac":"40:BD:32:CB:7B:4F","active":"1","UID":"landevice1","guest":"","ethernet":"","wlan":""},
			{"name":"nas","ip":"192.168.178.150","mac":"00:11:32:5e:e6:74","active":"1","UID":"landevice2","guest":"","ethernet":"","wlan":""},
			{"name":"phone","ip":"192.168.178.69","mac":"AE:6A:41:A7:B8:B8","active":"1","UID":"landevice3","guest":"","ethernet":"","wlan":""},
			{"name":"old","ip":"","mac":"","active":"0","UID":"landevice4","guest":"","ethernet":"","wlan":""}]}`))
	})
	mux.HandleFunc("/tr64desc.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<root><device><serviceList><service>
			<serviceType>urn:dslforum-org:service:Hosts:1</serviceType>
			<controlURL>/upnp/control/hosts</controlURL><SCPDURL>/hostsSCPD.xml</SCPDURL>
			</service></serviceList></device></root>`))
	})
	mux.HandleFunc("/upnp/control/hosts", func(w http.ResponseWriter, r *http.Request) {
		if !tr064OK {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>Invalid Action</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`))
			return
		}
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_GetHostListPathResponse xmlns:u="urn:dslforum-org:service:Hosts:1"><NewX_AVM-DE_HostListPath>/devicehostlist.lua?sid=abc</NewX_AVM-DE_HostListPath></u:X_AVM-DE_GetHostListPathResponse></s:Body></s:Envelope>`))
	})
	mux.HandleFunc("/devicehostlist.lua", func(w http.ResponseWriter, r *http.Request) {
		// The NAS is listed under another primary MAC; only its MAC list matches.
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?> <List>
<Item><MACAddress>40:BD:32:CB:7B:4F</MACAddress><InterfaceType>Ethernet</InterfaceType><X_AVM-DE_Guest>1</X_AVM-DE_Guest></Item>
<Item><MACAddress>00:11:32:5E:E6:75</MACAddress><X_AVM-DE_MACAddressList>00:11:32:5E:E6:75,00:11:32:5E:E6:74</X_AVM-DE_MACAddressList><InterfaceType>Ethernet</InterfaceType><X_AVM-DE_Guest>0</X_AVM-DE_Guest></Item>
<Item><MACAddress>AE:6A:41:A7:B8:B8</MACAddress><InterfaceType>802.11</InterfaceType><X_AVM-DE_Guest>0</X_AVM-DE_Guest></Item>
<Item><MACAddress></MACAddress><InterfaceType>Ethernet</InterfaceType><X_AVM-DE_Guest>1</X_AVM-DE_Guest></Item>
</List>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	prev := fritzbox.TR064Port
	fritzbox.TR064Port = port
	t.Cleanup(func() { fritzbox.TR064Port = prev })

	home := t.TempDir()
	t.Setenv("FRITZBOX_ADDRESS", srv.URL)
	t.Setenv("FRITZBOX_USERNAME", "u")
	t.Setenv("FRITZBOX_PASSWORD", "p")
	t.Setenv("FRITZBOX_SID", "abc")
	t.Setenv("FRITZBOX_CONFIG", filepath.Join(home, "config.toml"))
	t.Setenv("FRITZBOX_NO_LEARN", "true")
	return srv
}

func runHosts(t *testing.T, args ...string) ([]map[string]any, error) {
	t.Helper()
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append(args, "--agent", "--home", t.TempDir()))
	if err := root.Execute(); err != nil {
		return nil, err
	}
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decoding %q: %v", out.String(), err)
	}
	return env.Results, nil
}

func names(rows []map[string]any) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r["name"].(string))
	}
	return out
}

func TestHostsListMediumFromTR064(t *testing.T) {
	fakeBox(t, true)
	for medium, want := range map[string]string{"guest": "pfSense", "ethernet": "nas", "wifi": "phone", "unknown": "old"} {
		rows, err := runHosts(t, "hosts", "list", "--medium", medium)
		if err != nil {
			t.Fatalf("--medium %s: %v", medium, err)
		}
		if got := names(rows); len(got) != 1 || got[0] != want {
			t.Errorf("--medium %s = %v, want [%s]", medium, got, want)
		}
	}
}

func TestHostsListMediumFailsWhenTR064Fails(t *testing.T) {
	fakeBox(t, false)
	if rows, err := runHosts(t, "hosts", "list", "--medium", "guest"); err == nil {
		t.Fatalf("--medium guest without TR-064 returned %v and no error; an empty list would hide the failure", names(rows))
	}
	// Without a medium filter the list still works, medium falling back.
	rows, err := runHosts(t, "hosts", "list")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Errorf("hosts list without TR-064 = %v, want all 4 devices", names(rows))
	}
}
