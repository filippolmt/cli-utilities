// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"sort"
	"strings"

	"fritzbox-pp-cli/internal/fritzbox"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		fbAttach(root, newTR064Cmd(flags))
	})
}

// fbAttach adds cmd to parent unless a command with the same name is already
// registered, so a later generated command never collides with a hand-authored one.
func fbAttach(parent, cmd *cobra.Command) {
	for _, existing := range parent.Commands() {
		if existing.Name() == cmd.Name() {
			return
		}
	}
	rejectStrayArgs(cmd)
	parent.AddCommand(cmd)
}

// rejectStrayArgs makes every runnable command in the tree that does not name
// a positional in its Use line (e.g. "get <device>") refuse positionals, so
// 'wifi status 5' fails instead of silently acting on the --band default.
// Commands that do take positionals validate them in RunE.
func rejectStrayArgs(cmd *cobra.Command) {
	if cmd.Args == nil && cmd.Runnable() && !strings.Contains(cmd.Use, "<") {
		cmd.Args = cobra.NoArgs
	}
	for _, sub := range cmd.Commands() {
		rejectStrayArgs(sub)
	}
}

// fbFind resolves an existing command path so hand-authored subcommands can
// hang off a generated parent.
func fbFind(root *cobra.Command, path ...string) *cobra.Command {
	cmd, _, err := root.Find(path)
	if err != nil || cmd == nil {
		return nil
	}
	// Find falls back to the nearest ancestor when the leaf is unknown, so the
	// returned command's own name must match the leaf that was asked for.
	if cmd.Name() != path[len(path)-1] {
		return nil
	}
	return cmd
}

func newTR064Cmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tr064",
		Short: "Inspect and call the router's TR-064 control interface",
		Long: strings.Trim(`
TR-064 is the router's own control protocol. It describes itself: the box
publishes a catalog of services, and each service publishes the exact actions
and arguments the firmware in front of you supports.

These commands read that catalog and let you call any action on it, including
the vendor-specific extensions a frozen client library would not know about.
`, "\n"),
	}
	cmd.AddCommand(newTR064ServicesCmd(flags), newTR064ActionsCmd(flags), newTR064DescribeCmd(flags), newTR064CallCmd(flags))
	return cmd
}

func newTR064ServicesCmd(flags *rootFlags) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:         "services",
		Short:       "List every TR-064 service this router publishes",
		Example:     "  fritzbox-pp-cli tr064 services --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the router service catalog", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svcs, err := box.Services(ctx)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(svcs))
			for _, s := range svcs {
				row := map[string]any{"service": s.Name(), "service_type": s.Type, "control_url": s.ControlURL}
				if raw {
					row["scpd_url"] = s.SCPDURL
					row["service_id"] = s.ID
				}
				rows = append(rows, row)
			}
			return fbEmit(cmd, flags, rows, "This router published no TR-064 services.")
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Include the descriptor URL and service id for each service")
	return cmd
}

func newTR064ActionsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "actions <service>",
		Short:       "List the actions a TR-064 service exposes",
		Example:     "  fritzbox-pp-cli tr064 actions WANPPPConnection1 --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "service=DeviceInfo1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read a service descriptor", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a service name is required; run 'fritzbox-pp-cli tr064 services' to list them"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, err := box.Service(ctx, args[0])
			if err != nil {
				return err
			}
			actions, err := box.tr.Actions(ctx, svc)
			if err != nil {
				return fbErr(err)
			}
			rows := make([]map[string]any, 0, len(actions))
			for _, a := range actions {
				rows = append(rows, map[string]any{
					"service": svc.Name(),
					"action":  a.Name,
					"in":      argNames(a.In()),
					"out":     argNames(a.Out()),
				})
			}
			return fbEmit(cmd, flags, rows, fmt.Sprintf("Service %s exposes no actions.", svc.Name()))
		},
	}
	return cmd
}

func newTR064DescribeCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "describe <service> <action>",
		Short:       "Show the full argument contract of one TR-064 action",
		Example:     "  fritzbox-pp-cli tr064 describe WANPPPConnection1 GetInfo --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "service=DeviceInfo1;action=GetInfo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a service name and an action name are both required"))
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "describe a TR-064 action", map[string]any{"service": args[0], "action": args[1]})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, err := box.Service(ctx, args[0])
			if err != nil {
				return err
			}
			actions, err := box.tr.Actions(ctx, svc)
			if err != nil {
				return fbErr(err)
			}
			for _, a := range actions {
				if !strings.EqualFold(a.Name, args[1]) {
					continue
				}
				return fbEmitObject(cmd, flags, map[string]any{
					"service":   svc.Name(),
					"action":    a.Name,
					"arguments": a.Arguments,
				})
			}
			return notFoundErr(fmt.Errorf("service %s has no action named %q", svc.Name(), args[1]))
		},
	}
	return cmd
}

func newTR064CallCmd(flags *rootFlags) *cobra.Command {
	var argPairs []string
	cmd := &cobra.Command{
		Use:   "call <service> <action>",
		Short: "Call any TR-064 action on this router",
		Long: strings.Trim(`
Call any action the router publishes, including vendor extensions.

Arguments are supplied as repeated --arg key=value pairs. The "New" prefix that
FRITZ!OS uses on the wire is added automatically, so --arg NewIndex=0 and
--arg Index=0 are equivalent.

Use this command when no dedicated wrapper exists. Do NOT use it to discover
what is callable; use 'tr064 actions' or 'actions search' instead.
`, "\n"),
		Example:     "  fritzbox-pp-cli tr064 call DeviceInfo1 GetInfo --agent",
		Annotations: map[string]string{"pp:happy-args": "service=DeviceInfo1;action=GetInfo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a service name and an action name are both required"))
			}
			parsed, err := parseArgPairs(argPairs)
			if err != nil {
				return usageErr(err)
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "call a TR-064 action", map[string]any{
					"service": args[0], "action": args[1], "arguments": parsed,
				})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, args[0], args[1], parsed)
			if err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, soapToRow(out))
		},
	}
	cmd.Flags().StringArrayVar(&argPairs, "arg", nil, "Action argument as key=value; repeat for multiple arguments")
	return cmd
}

func parseArgPairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		// SplitN, not Cut on every '=', so a value containing '=' survives.
		key, value, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--arg %q is not in key=value form", p)
		}
		out[strings.TrimSpace(key)] = value
	}
	return out, nil
}

func argNames(args []fritzbox.Argument) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, strings.TrimPrefix(a.Name, "New"))
	}
	sort.Strings(out)
	return out
}
