// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		fbAttach(root, newHomeCmd(flags))
		fbAttach(root, newAhaCmd(flags))
	})
}

// ahaDevice is one smart-home actor as reported by getdevicelistinfos.
//
// Every measurement the interface reports is a scaled integer: temperatures in
// tenths of a degree, thermostat setpoints in half degrees, power in
// milliwatts. Decoding is done once here so no command has to remember which
// scale applies to which field.
type ahaDevice struct {
	Identifier   string `xml:"identifier,attr"`
	ID           string `xml:"id,attr"`
	FunctionMask string `xml:"functionbitmask,attr"`
	FWVersion    string `xml:"fwversion,attr"`
	Manufacturer string `xml:"manufacturer,attr"`
	ProductName  string `xml:"productname,attr"`
	Present      string `xml:"present"`
	Name         string `xml:"name"`
	Switch       struct {
		State string `xml:"state"`
		Mode  string `xml:"mode"`
	} `xml:"switch"`
	PowerMeter struct {
		Power   string `xml:"power"`
		Energy  string `xml:"energy"`
		Voltage string `xml:"voltage"`
	} `xml:"powermeter"`
	Temperature struct {
		Celsius string `xml:"celsius"`
		Offset  string `xml:"offset"`
	} `xml:"temperature"`
	HKR struct {
		TIst       string `xml:"tist"`
		TSoll      string `xml:"tsoll"`
		Komfort    string `xml:"komfort"`
		Absenk     string `xml:"absenk"`
		Battery    string `xml:"battery"`
		BatteryLow string `xml:"batterylow"`
	} `xml:"hkr"`
	SimpleOnOff struct {
		State string `xml:"state"`
	} `xml:"simpleonoff"`
}

func (d ahaDevice) row() map[string]any {
	row := map[string]any{
		"ain":     d.Identifier,
		"name":    d.Name,
		"product": d.ProductName,
		"present": d.Present == "1",
	}
	if d.Switch.State != "" {
		row["switch_on"] = d.Switch.State == "1"
	}
	if v, ok := scaledFloat(d.PowerMeter.Power, 1000); ok {
		row["power_watts"] = v
	}
	if v, ok := scaledFloat(d.PowerMeter.Energy, 1); ok {
		row["energy_wh"] = v
	}
	if v, ok := scaledFloat(d.Temperature.Celsius, 10); ok {
		row["temperature_c"] = v
	}
	if v, ok := scaledFloat(d.HKR.TIst, 2); ok {
		row["thermostat_current_c"] = v
	}
	if v, ok := hkrSetpoint(d.HKR.TSoll); ok {
		row["thermostat_target_c"] = v
	} else if d.HKR.TSoll == "253" {
		row["thermostat_target_c"] = "off"
	} else if d.HKR.TSoll == "254" {
		row["thermostat_target_c"] = "max"
	}
	if d.HKR.Battery != "" {
		row["battery_percent"] = d.HKR.Battery
		row["battery_low"] = d.HKR.BatteryLow == "1"
	}
	return row
}

// scaledFloat divides an integer-encoded measurement by its scale.
func scaledFloat(raw string, scale float64) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return n / scale, true
}

// hkrSetpoint decodes a thermostat setpoint. Values 253 and 254 are the
// sentinels for "off" and "fully open" rather than temperatures, so they are
// rejected here and rendered as words by the caller.
func hkrSetpoint(raw string) (float64, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n >= 253 {
		return 0, false
	}
	return float64(n) / 2, true
}

// HomeDevices fetches the smart-home actor list.
func (b *fbBox) HomeDevices(ctx context.Context) ([]ahaDevice, error) {
	body, err := b.web.AHA(ctx, "getdevicelistinfos", nil)
	if err != nil {
		return nil, fbErr(err)
	}
	var doc struct {
		Devices []ahaDevice `xml:"device"`
		Groups  []ahaDevice `xml:"group"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the smart-home device list: %w", err))
	}
	return append(doc.Devices, doc.Groups...), nil
}

// resolveAIN turns a device name or partial AIN into the exact AIN the
// interface expects, so users are not forced to copy 12-digit identifiers.
func (b *fbBox) resolveAIN(ctx context.Context, needle string) (string, error) {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return "", usageErr(fmt.Errorf("a device name or AIN is required"))
	}
	devices, err := b.HomeDevices(ctx)
	if err != nil {
		return "", err
	}
	var partial []ahaDevice
	for _, d := range devices {
		if d.Identifier == needle || strings.EqualFold(d.Name, needle) {
			return d.Identifier, nil
		}
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(needle)) {
			partial = append(partial, d)
		}
	}
	switch len(partial) {
	case 0:
		return "", notFoundErr(fmt.Errorf("no smart-home device matching %q; run 'fritzbox-pp-cli home devices' to list them", needle))
	case 1:
		return partial[0].Identifier, nil
	default:
		return "", usageErr(fmt.Errorf("%q matches %d smart-home devices; use the AIN instead", needle, len(partial)))
	}
}

func newHomeCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "home",
		Short: "Smart-home actors: switches, thermostats, energy, templates",
		Long: `Control the smart-home actors paired with this router.

Devices are addressed by name or by AIN. If a command reports that no devices
exist, none are paired with the router yet; pair one in the FRITZ!Box user
interface under Smart Home first.`,
	}
	cmd.AddCommand(newHomeDevicesCmd(flags), newHomeSwitchCmd(flags), newHomePowerCmd(flags),
		newHomeThermostatCmd(flags), newHomeStatsCmd(flags), newHomeTemplatesCmd(flags),
		newHomeTriggersCmd(flags), newHomeLevelCmd(flags), newHomeColorCmd(flags))
	return cmd
}

func newHomeDevicesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "devices",
		Short:       "List the smart-home actors paired with this router",
		Example:     "  fritzbox-pp-cli home devices --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the smart-home actors", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			devices, err := box.HomeDevices(ctx)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(devices))
			for _, d := range devices {
				rows = append(rows, d.row())
			}
			return fbEmit(cmd, flags, rows, "No smart-home actors are paired with this router.")
		},
	}
	return cmd
}

func newHomeSwitchCmd(flags *rootFlags) *cobra.Command {
	var on, off, toggle, confirm bool
	cmd := &cobra.Command{
		Use:         "switch <device>",
		Short:       "Read or change a smart switch",
		Example:     "  fritzbox-pp-cli home switch lamp --agent",
		Annotations: map[string]string{"pp:happy-args": "device=lamp"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or change a smart switch", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			count := 0
			for _, b := range []bool{on, off, toggle} {
				if b {
					count++
				}
			}
			if count > 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--on, --off, and --toggle are mutually exclusive"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			if count == 0 {
				state, err := box.web.AHA(ctx, "getswitchstate", map[string]string{"ain": ain})
				if err != nil {
					return fbErr(err)
				}
				return fbEmitObject(cmd, flags, map[string]any{"ain": ain, "on": strings.TrimSpace(string(state)) == "1"})
			}
			verb := map[bool]string{true: "setswitchon"}[on]
			if off {
				verb = "setswitchoff"
			}
			if toggle {
				verb = "setswitchtoggle"
			}
			action := fmt.Sprintf("change smart switch %s", ain)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"ain": ain, "command": verb})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			out, err := box.web.AHA(ctx, verb, map[string]string{"ain": ain})
			if err != nil {
				return fbErr(err)
			}
			return fbEmitObject(cmd, flags, map[string]any{"ain": ain, "on": strings.TrimSpace(string(out)) == "1", "changed": true})
		},
	}
	cmd.Flags().BoolVar(&on, "on", false, "Turn the switch on")
	cmd.Flags().BoolVar(&off, "off", false, "Turn the switch off")
	cmd.Flags().BoolVar(&toggle, "toggle", false, "Flip the switch to its other state")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func newHomePowerCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "power <device>",
		Short:       "Show current power draw and cumulative energy for a switch",
		Example:     "  fritzbox-pp-cli home power lamp --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "device=lamp"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read smart-switch power draw", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			row := map[string]any{"ain": ain}
			if p, err := box.web.AHA(ctx, "getswitchpower", map[string]string{"ain": ain}); err == nil {
				if v, ok := scaledFloat(string(p), 1000); ok {
					row["power_watts"] = v
				}
			}
			if e, err := box.web.AHA(ctx, "getswitchenergy", map[string]string{"ain": ain}); err == nil {
				if v, ok := scaledFloat(string(e), 1); ok {
					row["energy_wh"] = v
				}
			}
			if len(row) == 1 {
				return apiErr(fmt.Errorf("device %s reported no power measurements; it may not have a power meter", ain))
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	return cmd
}

func newHomeThermostatCmd(flags *rootFlags) *cobra.Command {
	var target float64
	var boost, windowOpen, off, confirm bool
	cmd := &cobra.Command{
		Use:   "thermostat <device>",
		Short: "Read or set a radiator thermostat",
		Long: `Read a radiator thermostat's current and target temperature, or change it.

FRITZ!OS accepts setpoints in half-degree steps between 8 and 28 degrees; a
value outside that range is clamped by the router to its nearest limit.`,
		Example:     "  fritzbox-pp-cli home thermostat bedroom --agent",
		Annotations: map[string]string{"pp:happy-args": "device=bedroom"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or set a thermostat", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			write := target > 0 || boost || windowOpen || off
			if !write {
				row := map[string]any{"ain": ain}
				if t, err := box.web.AHA(ctx, "gethkrtsoll", map[string]string{"ain": ain}); err == nil {
					if v, ok := hkrSetpoint(string(t)); ok {
						row["target_c"] = v
					} else {
						row["target_c"] = hkrSentinel(string(t))
					}
				}
				if t, err := box.web.AHA(ctx, "gettemperature", map[string]string{"ain": ain}); err == nil {
					if v, ok := scaledFloat(string(t), 10); ok {
						row["current_c"] = v
					}
				}
				if len(row) == 1 {
					return apiErr(fmt.Errorf("device %s reported no thermostat values; it may not be a radiator control", ain))
				}
				return fbEmitObject(cmd, flags, row)
			}

			switchcmd, params := "sethkrtsoll", map[string]string{"ain": ain}
			switch {
			case off:
				params["param"] = "253"
			case boost:
				params["param"] = "254"
			case windowOpen:
				switchcmd = "sethkrwindowopen"
				params["endtimestamp"] = "0"
			default:
				// FRITZ!OS counts in half degrees, so the setpoint is doubled
				// and rounded to the nearest step it can actually represent.
				params["param"] = strconv.Itoa(int(target*2 + 0.5))
			}
			action := fmt.Sprintf("change thermostat %s", ain)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"ain": ain, "command": switchcmd})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			out, err := box.web.AHA(ctx, switchcmd, params)
			if err != nil {
				return fbErr(err)
			}
			row := map[string]any{"ain": ain, "changed": true}
			if v, ok := hkrSetpoint(string(out)); ok {
				row["target_c"] = v
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	cmd.Flags().Float64Var(&target, "set", 0, "Target temperature in degrees Celsius")
	cmd.Flags().BoolVar(&boost, "boost", false, "Open the valve fully")
	cmd.Flags().BoolVar(&off, "off", false, "Turn the radiator off")
	cmd.Flags().BoolVar(&windowOpen, "windowopen", false, "Trigger the window-open pause")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func hkrSentinel(raw string) string {
	switch strings.TrimSpace(raw) {
	case "253":
		return "off"
	case "254":
		return "max"
	default:
		return "unknown"
	}
}

func newHomeStatsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "stats <device>",
		Short:       "Show the recorded temperature and energy history for a device",
		Example:     "  fritzbox-pp-cli home stats lamp --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "device=lamp"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read smart-home device statistics", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			body, err := box.web.AHA(ctx, "getbasicdevicestats", map[string]string{"ain": ain})
			if err != nil {
				return fbErr(err)
			}
			var doc struct {
				Series []struct {
					XMLName xml.Name
					Stats   []struct {
						Count    string `xml:"count,attr"`
						Grid     string `xml:"grid,attr"`
						Datatime string `xml:"datatime,attr"`
						Values   string `xml:",chardata"`
					} `xml:"stats"`
				} `xml:",any"`
			}
			if err := xml.Unmarshal(body, &doc); err != nil {
				return apiErr(fmt.Errorf("parsing the device statistics: %w", err))
			}
			rows := make([]map[string]any, 0)
			for _, series := range doc.Series {
				for _, s := range series.Stats {
					rows = append(rows, map[string]any{
						"series": series.XMLName.Local, "count": s.Count,
						"grid_seconds": s.Grid, "values": strings.TrimSpace(s.Values),
					})
				}
			}
			return fbEmit(cmd, flags, rows, "This device has no recorded statistics.")
		},
	}
	return cmd
}

func newHomeTemplatesCmd(flags *rootFlags) *cobra.Command {
	var apply string
	var confirm bool
	cmd := &cobra.Command{
		Use:         "templates",
		Short:       "List smart-home templates, or apply one",
		Example:     "  fritzbox-pp-cli home templates --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list or apply smart-home templates", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if apply != "" {
				action := fmt.Sprintf("apply smart-home template %s", apply)
				if !confirm {
					return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"template": apply})
				}
				if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
					return err
				}
				if _, err := box.web.AHA(ctx, "applytemplate", map[string]string{"ain": apply}); err != nil {
					return fbErr(err)
				}
				return fbEmitObject(cmd, flags, map[string]any{"template": apply, "applied": true})
			}
			body, err := box.web.AHA(ctx, "gettemplatelistinfos", nil)
			if err != nil {
				return fbErr(err)
			}
			var doc struct {
				Templates []struct {
					Identifier string `xml:"identifier,attr"`
					ID         string `xml:"id,attr"`
					Name       string `xml:"name"`
				} `xml:"template"`
			}
			if err := xml.Unmarshal(body, &doc); err != nil {
				return apiErr(fmt.Errorf("parsing the template list: %w", err))
			}
			rows := make([]map[string]any, 0, len(doc.Templates))
			for _, t := range doc.Templates {
				rows = append(rows, map[string]any{"ain": t.Identifier, "id": t.ID, "name": t.Name})
			}
			return fbEmit(cmd, flags, rows, "This router has no smart-home templates.")
		},
	}
	cmd.Flags().StringVar(&apply, "apply", "", "Apply the template with this AIN")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the template instead of printing what would happen")
	return cmd
}

func newHomeTriggersCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "triggers",
		Short:       "List the smart-home triggers defined on this router",
		Example:     "  fritzbox-pp-cli home triggers --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list smart-home triggers", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			body, err := box.web.AHA(ctx, "gettriggerlistinfos", nil)
			if err != nil {
				return fbErr(err)
			}
			var doc struct {
				Triggers []struct {
					Identifier string `xml:"identifier,attr"`
					Active     string `xml:"active,attr"`
					Name       string `xml:"name"`
				} `xml:"trigger"`
			}
			if err := xml.Unmarshal(body, &doc); err != nil {
				return apiErr(fmt.Errorf("parsing the trigger list: %w", err))
			}
			rows := make([]map[string]any, 0, len(doc.Triggers))
			for _, t := range doc.Triggers {
				rows = append(rows, map[string]any{"ain": t.Identifier, "name": t.Name, "active": t.Active == "1"})
			}
			return fbEmit(cmd, flags, rows, "This router has no smart-home triggers.")
		},
	}
	return cmd
}

func newHomeLevelCmd(flags *rootFlags) *cobra.Command {
	var percent int
	var confirm bool
	cmd := &cobra.Command{
		Use:         "level <device>",
		Short:       "Set the brightness or level of a dimmable actor",
		Example:     "  fritzbox-pp-cli home level lamp --percent 40 --confirm",
		Annotations: map[string]string{"pp:happy-args": "device=lamp;--percent=40"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "set an actor's level", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			if percent < 0 || percent > 100 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--percent must be between 0 and 100"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			action := fmt.Sprintf("set actor %s to %d percent", ain, percent)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"ain": ain, "percent": percent})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			if _, err := box.web.AHA(ctx, "setlevelpercentage", map[string]string{"ain": ain, "level": strconv.Itoa(percent)}); err != nil {
				return fbErr(err)
			}
			return fbEmitObject(cmd, flags, map[string]any{"ain": ain, "percent": percent, "changed": true})
		},
	}
	cmd.Flags().IntVar(&percent, "percent", 0, "Level as a percentage from 0 to 100")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func newHomeColorCmd(flags *rootFlags) *cobra.Command {
	var hue, saturation, duration int
	var confirm bool
	cmd := &cobra.Command{
		Use:   "color <device>",
		Short: "Set the colour of a colour-capable lamp",
		Long: `Set a lamp's colour by hue and saturation.

FRITZ!OS only accepts hue and saturation pairs from its own predefined palette;
arbitrary combinations are snapped to the nearest supported colour by the
router rather than rejected.`,
		Example:     "  fritzbox-pp-cli home color lamp --hue 358 --saturation 180 --confirm",
		Annotations: map[string]string{"pp:happy-args": "device=lamp;--hue=358;--saturation=180"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "set an actor's colour", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name or AIN is required"))
			}
			if hue < 0 || hue > 359 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--hue must be between 0 and 359"))
			}
			if saturation < 0 || saturation > 255 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--saturation must be between 0 and 255"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			ain, err := box.resolveAIN(ctx, args[0])
			if err != nil {
				return err
			}
			action := fmt.Sprintf("set the colour of actor %s", ain)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"ain": ain, "hue": hue, "saturation": saturation})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			if _, err := box.web.AHA(ctx, "setcolor", map[string]string{
				"ain": ain, "hue": strconv.Itoa(hue), "saturation": strconv.Itoa(saturation),
				"duration": strconv.Itoa(duration),
			}); err != nil {
				return fbErr(err)
			}
			return fbEmitObject(cmd, flags, map[string]any{"ain": ain, "hue": hue, "saturation": saturation, "changed": true})
		},
	}
	cmd.Flags().IntVar(&hue, "hue", 0, "Hue from 0 to 359")
	cmd.Flags().IntVar(&saturation, "saturation", 0, "Saturation from 0 to 255")
	cmd.Flags().IntVar(&duration, "duration", 0, "Transition duration in hundredths of a second")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

// ------------------------------------------------------------------- aha ----

func newAhaCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "aha", Short: "Raw access to the smart-home HTTP interface"}
	var params []string
	call := &cobra.Command{
		Use:   "call <switchcmd>",
		Short: "Issue any smart-home interface command and print the raw response",
		Long: `Issue any AHA command directly.

Use this when no dedicated wrapper exists. Do NOT use it for ordinary switch or
thermostat control; the 'home' commands decode the router's scaled integers into
real units, which this command deliberately does not.`,
		Example:     "  fritzbox-pp-cli aha call getswitchlist",
		Annotations: map[string]string{"pp:happy-args": "switchcmd=getswitchlist"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "issue a raw smart-home command", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a switchcmd name is required, for example getdevicelistinfos"))
			}
			parsed, err := parseArgPairs(params)
			if err != nil {
				return usageErr(err)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			body, err := box.web.AHA(ctx, args[0], parsed)
			if err != nil {
				return fbErr(err)
			}
			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, map[string]any{
					"switchcmd": args[0], "response": strings.TrimSpace(string(body)),
				}, flags)
			}
			fmt.Fprintln(out, strings.TrimSpace(string(body)))
			return nil
		},
	}
	call.Flags().StringArrayVar(&params, "param", nil, "Extra query parameter as key=value; repeat for multiple")
	cmd.AddCommand(call)
	return cmd
}
