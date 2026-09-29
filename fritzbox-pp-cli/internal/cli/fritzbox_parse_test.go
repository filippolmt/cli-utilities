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

func TestHandWrittenCommandsRejectStrayPositionalArgs(t *testing.T) {
	root := RootCmd()
	// Generated parents (hosts, portmap, calls, log, wan, ...) are upstream's
	// business; only the hand-written commands hung off them are checked.
	generatedParents := map[string]bool{"hosts": true, "portmap": true, "calls": true, "log": true, "wan": true, "dect": true, "tam": true, "wlan": true}
	var check func(*cobra.Command)
	check = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			check(sub)
		}
		if !c.Runnable() {
			return
		}
		if strings.Contains(c.Use, "<") {
			return // takes positionals and validates them in RunE
		}
		if c.Args == nil || c.Args(c, []string{"5"}) == nil {
			t.Errorf("%s accepts a stray positional argument", c.CommandPath())
		}
	}
	for _, top := range root.Commands() {
		if generatedParents[top.Name()] {
			for _, sub := range top.Commands() {
				check(sub)
			}
			continue
		}
		if handWrittenTop[top.Name()] {
			check(top)
		}
	}
}

// handWrittenTop lists the top-level commands this CLI adds by hand.
var handWrittenTop = map[string]bool{
	"wifi": true, "device": true, "tr064": true, "home": true, "vpn": true, "snapshot": true,
	"metrics": true, "energy": true, "health": true, "mesh": true, "presence": true,
	"actions": true, "dsl": true, "lan": true, "phonebook": true, "aha": true,
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
