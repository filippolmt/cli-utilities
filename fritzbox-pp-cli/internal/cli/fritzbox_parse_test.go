package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseProfileListXML(t *testing.T) {
	raw := "<FilterProfileList><FilterProfile><FilterProfileID>filtprof2</FilterProfileID><Name></Name><FilterProfileType>Guest</FilterProfileType><TimeTable /></FilterProfile>" +
		"<FilterProfile><FilterProfileID>filtprof4</FilterProfileID><Name>Kids</Name><FilterProfileType>Standard</FilterProfileType></FilterProfile></FilterProfileList>\n"
	rows := parseProfileList(raw)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %v", len(rows), rows)
	}
	if rows[0]["id"] != "filtprof2" || rows[0]["name"] != "Guest" || rows[0]["type"] != "Guest" {
		t.Errorf("row 0 = %v; an empty name should fall back to the profile type", rows[0])
	}
	if rows[1]["id"] != "filtprof4" || rows[1]["name"] != "Kids" {
		t.Errorf("row 1 = %v", rows[1])
	}
}

func TestParseEnergyDrainNamesUnnamedPorts(t *testing.T) {
	list := json.RawMessage(`[
		{"cumPerc":68,"actPerc":67,"name":"CPU","statuses":"125 MHz"},
		{"statuses":"1 device","name":"","lan":[{"name":"WAN","class":"green"}]}
	]`)
	rows, err := parseEnergyDrain(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "CPU" || rows[1].Name != "WAN" {
		t.Fatalf("got %+v; an unnamed subsystem should take its port names", rows)
	}
}

func TestExtractWireguardRowsFromShareWireguard(t *testing.T) {
	var decoded any
	payload := `{"init":{"wg_public_key":"x","type":"IPSec Xauth PSK","userConnections":[],
		"boxConnections":{"connection1":{"type":"wireguard_lan_lan","active":true,"connected":true,"name":"Oracle"}}}}`
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatal(err)
	}
	rows := extractWireguardRows(decoded)
	if len(rows) != 1 || rows[0]["name"] != "Oracle" || rows[0]["connected"] != true {
		t.Fatalf("got %v", rows)
	}
}

func TestLedRow(t *testing.T) {
	raw := json.RawMessage(`{"data":{"ledSettings":{"ledDisplay":"2","hasEnv":"1","dimValue":"3","canDim":"1","envLight":"1"}}}`)
	row, err := ledRow(raw)
	if err != nil {
		t.Fatal(err)
	}
	if row["leds_on"] != false || row["brightness"] != "3" || row["led_display"] != "2" || row["ambient_light"] != true {
		t.Fatalf("got %v", row)
	}
}

func TestCommandsRejectStrayPositionalArgs(t *testing.T) {
	root := RootCmd()
	var check func(*cobra.Command)
	check = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			check(sub)
		}
		if c == root || !c.Runnable() || strings.ContainsAny(c.Use, "<[") {
			return // positionals, when taken, are validated in RunE
		}
		if c.Args == nil || c.Args(c, []string{"5"}) == nil {
			t.Errorf("%s accepts a stray positional argument", c.CommandPath())
		}
	}
	check(root)
}

func TestParsePortOverview(t *testing.T) {
	raw := json.RawMessage(`{"data":{"devices":[
		{"devicename":"sy","localIpv4":"192.168.178.150","exposed_ipv4":false,
		 "igdrules":{"UDP_ipv4":"6881","TCP_ipv4":"16881, 62198","TCP_ipv6":"","GRP_ipv4":""}},
		{"devicename":"dmz","localIpv4":"192.168.178.9","exposed_ipv4":true,"igdrules":{}}
	]}}`)
	got := parsePortOverview(raw)
	want := []fbPortMapping{
		{Index: -1, Description: "sy (UPnP/PCP)", Protocol: "TCP", ExternalPort: "16881", InternalHost: "192.168.178.150", Enabled: true, Source: "upnp"},
		{Index: -1, Description: "sy (UPnP/PCP)", Protocol: "TCP", ExternalPort: "62198", InternalHost: "192.168.178.150", Enabled: true, Source: "upnp"},
		{Index: -1, Description: "sy (UPnP/PCP)", Protocol: "UDP", ExternalPort: "6881", InternalHost: "192.168.178.150", Enabled: true, Source: "upnp"},
		{Index: -1, Description: "dmz (exposed host)", Protocol: "ALL", ExternalPort: "*", InternalHost: "192.168.178.9", Enabled: true, Source: "exposed-host"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mappings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mapping %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFbEmitLabelsDataSource(t *testing.T) {
	flags := &rootFlags{agent: true, asJSON: true}
	var buf strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	if err := fbEmitObject(cmd, flags, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := fbEmitFrom(cmd, flags, "local", []map[string]any{{"a": 1}}, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"source":"live"`) && !strings.Contains(out, `"source": "live"`) {
		t.Errorf("router reads should be labelled live: %s", out)
	}
	if !strings.Contains(out, `"source":"local"`) && !strings.Contains(out, `"source": "local"`) {
		t.Errorf("stored reads should be labelled local: %s", out)
	}
}

func TestRedactSecrets(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(`{"guestAccess":{"psk":"hunter2","ssid":"Ospiti","empty_psk":""},
		"users":[{"name":"filippo","password":"x"}],"wg_public_key":"abc","wg_private_key":"def",
		"wps_pin":12345670,"api_token":"t","pwd":"p","bypass":"on","knownWlanDevices":[1],
		"field":{"name":"psk","value":"hunter3"},"other":{"name":"ssid","value":"Home"}}`), &v); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(redactSecrets(v))
	want := `{"api_token":"***","bypass":"on","field":{"name":"psk","value":"***"},` +
		`"guestAccess":{"empty_psk":"","psk":"***","ssid":"Ospiti"},"knownWlanDevices":[1],` +
		`"other":{"name":"ssid","value":"Home"},"pwd":"***","users":[{"name":"filippo","password":"***"}],` +
		`"wg_private_key":"***","wg_public_key":"abc","wps_pin":"***"}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestParseUnusedHosts(t *testing.T) {
	raw := json.RawMessage(`{"data":{"active":[{"name":"Mac"}],"passive":[
		{"UID":"landevice1","name":"phone","mac":"02:00:00:00:00:01","options":{"deleteable":true},"ipv4":{"ip":"192.168.178.10"}},
		{"UID":"landevice2","name":"printer","mac":"02:00:00:00:00:02","options":{"deleteable":false},"ipv4":{"ip":""}}
	]}}`)
	got, err := parseUnusedHosts(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []fbUnusedHost{
		{UID: "landevice1", Name: "phone", MAC: "02:00:00:00:00:01", IP: "192.168.178.10", Deletable: true},
		{UID: "landevice2", Name: "printer", MAC: "02:00:00:00:00:02", Deletable: false},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	// A device that went offline meanwhile (landevice3) must not hide that
	// landevice1 went.
	gone := goneHosts(got, []fbUnusedHost{{UID: "landevice2"}, {UID: "landevice3"}})
	if len(gone) != 1 || gone[0].UID != "landevice1" {
		t.Errorf("gone = %+v", gone)
	}
}
