package cli

import (
	"encoding/json"
	"testing"
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

func TestWifiBandCommandsRejectPositionalArgs(t *testing.T) {
	wifi := newWifiCmd(&rootFlags{})
	for _, sub := range wifi.Commands() {
		if sub.Args == nil {
			t.Errorf("wifi %s accepts positional arguments silently", sub.Name())
			continue
		}
		if err := sub.Args(sub, []string{"5"}); err == nil {
			t.Errorf("wifi %s accepted a positional argument", sub.Name())
		}
	}
}
