// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent := fbFind(root, "calls"); parent != nil {
			fbAttach(parent, newCallsListCmd(flags))
			fbAttach(parent, newCallsDeflectionCmd(flags))
		}
		fbAttach(root, newPhonebookCmd(flags))
		if parent := fbFind(root, "tam"); parent != nil {
			fbAttach(parent, newTamToggleCmd(flags, true))
			fbAttach(parent, newTamToggleCmd(flags, false))
			fbAttach(parent, newTamMessagesCmd(flags))
		}
		if parent := fbFind(root, "dect"); parent != nil {
			fbAttach(parent, newDectSignalCmd(flags))
		}
	})
}

// ----------------------------------------------------------------- calls ----

// callTypes maps the numeric call type FRITZ!OS reports onto a readable label.
var callTypes = map[string]string{
	"1": "inbound", "2": "missed", "3": "outbound",
	"9": "active-inbound", "10": "rejected", "11": "active-outbound",
}

type rawCall struct {
	ID       string `xml:"Id"`
	Type     string `xml:"Type"`
	Caller   string `xml:"Caller"`
	Called   string `xml:"Called"`
	Name     string `xml:"Name"`
	Date     string `xml:"Date"`
	Duration string `xml:"Duration"`
	Port     string `xml:"Port"`
}

// fbCall is one normalised call-journal entry.
type fbCall struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	Number   string    `json:"number"`
	At       time.Time `json:"at"`
	Duration string    `json:"duration"`
	Port     string    `json:"port"`
}

func (c fbCall) row() map[string]any {
	at := ""
	if !c.At.IsZero() {
		at = c.At.Format(time.RFC3339)
	}
	return map[string]any{
		"at": at, "kind": c.Kind, "name": c.Name,
		"number": c.Number, "duration": c.Duration, "id": c.ID,
	}
}

// parseFritzDate decodes the "dd.mm.yy HH:MM" stamp FRITZ!OS writes into call
// and message lists. It is interpreted in the local zone, which is the zone the
// router itself renders in.
func parseFritzDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{"02.01.06 15:04", "02.01.2006 15:04", "02.01.06 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Calls fetches the router's call journal.
//
// GetCallList does not return the journal: it returns a URL that already
// carries a session id, and the entries are only available from that second
// hop. A client that stops at the SOAP response reports an empty journal.
func (b *fbBox) Calls(ctx context.Context, days int) ([]fbCall, error) {
	args := map[string]string{}
	if days > 0 {
		args["Days"] = strconv.Itoa(days)
	}
	out, err := b.Call(ctx, "X_AVM-DE_OnTel1", "GetCallList", args)
	if err != nil {
		return nil, err
	}
	listURL := out["CallListURL"]
	if listURL == "" {
		return nil, apiErr(fmt.Errorf("router returned no call-list URL"))
	}
	if days > 0 && !strings.Contains(listURL, "days=") {
		listURL += "&days=" + strconv.Itoa(days)
	}
	body, err := b.web.Fetch(ctx, listURL)
	if err != nil {
		return nil, fbErr(err)
	}
	var doc struct {
		Calls []rawCall `xml:"Call"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the call journal: %w", err))
	}
	calls := make([]fbCall, 0, len(doc.Calls))
	for _, c := range doc.Calls {
		kind := callTypes[c.Type]
		if kind == "" {
			kind = "type-" + c.Type
		}
		// The remote party is the caller on an inbound call and the called
		// number on an outbound one; taking the wrong side reports the
		// household's own number for half the journal.
		number := c.Caller
		if kind == "outbound" || kind == "active-outbound" {
			number = c.Called
		}
		calls = append(calls, fbCall{
			ID: c.ID, Kind: kind, Name: strings.TrimSpace(c.Name),
			Number: strings.TrimSpace(number), At: parseFritzDate(c.Date),
			Duration: c.Duration, Port: c.Port,
		})
	}
	return calls, nil
}

func newCallsListCmd(flags *rootFlags) *cobra.Command {
	var days, limit int
	var kind string
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List recent calls with name, number, and duration",
		Example:     "  fritzbox-pp-cli calls list --days 7 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the call journal", map[string]any{"days": days})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			calls, err := box.Calls(ctx, days)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(calls))
			for _, c := range calls {
				if kind != "" && !strings.EqualFold(c.Kind, kind) {
					continue
				}
				rows = append(rows, c.row())
				if limit > 0 && len(rows) >= limit {
					break
				}
			}
			return fbEmit(cmd, flags, rows, "No calls in the router's journal for that period.")
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "How many days back to ask the router for")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of calls to return; 0 means all")
	cmd.Flags().StringVar(&kind, "kind", "", "Only show calls of this kind: inbound, outbound, missed, or rejected")
	return cmd
}

func newCallsDeflectionCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "deflection",
		Short:       "List the configured call deflections",
		Example:     "  fritzbox-pp-cli calls deflection --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list call deflections", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			count, err := box.Call(ctx, "X_AVM-DE_OnTel1", "GetNumberOfDeflections", nil)
			if err != nil {
				return err
			}
			n, _ := strconv.Atoi(count["NumberOfDeflections"])
			if n == 0 {
				return fbEmit(cmd, flags, nil, "No call deflections are configured.")
			}
			out, err := box.Call(ctx, "X_AVM-DE_OnTel1", "GetDeflections", nil)
			if err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"count": n, "deflections": out["DeflectionList"]})
		},
	}
	return cmd
}

// ------------------------------------------------------------- phonebook ----

type rawContact struct {
	Person struct {
		RealName string `xml:"realName"`
	} `xml:"person"`
	Telephony struct {
		Numbers []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"number"`
	} `xml:"telephony"`
}

// fbContact is one normalised phonebook number.
type fbContact struct {
	Book   string `json:"book"`
	Name   string `json:"name"`
	Number string `json:"number"`
	Kind   string `json:"kind"`
}

// Phonebook fetches every phonebook the router holds.
//
// As with the call journal, GetPhonebook answers with a download URL rather
// than the data itself, so the entries require a second hop.
func (b *fbBox) Phonebook(ctx context.Context) ([]fbContact, error) {
	list, err := b.Call(ctx, "X_AVM-DE_OnTel1", "GetPhonebookList", nil)
	if err != nil {
		return nil, err
	}
	ids := strings.Split(strings.TrimSpace(list["PhonebookList"]), ",")
	var contacts []fbContact
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		meta, err := b.Call(ctx, "X_AVM-DE_OnTel1", "GetPhonebook", map[string]string{"PhonebookID": id})
		if err != nil {
			// One unreadable phonebook must not hide the others.
			continue
		}
		url := meta["PhonebookURL"]
		if url == "" {
			continue
		}
		body, err := b.web.Fetch(ctx, url)
		if err != nil {
			continue
		}
		var doc struct {
			Contacts []rawContact `xml:"phonebook>contact"`
		}
		if err := xml.Unmarshal(body, &doc); err != nil {
			continue
		}
		bookName := meta["PhonebookName"]
		if bookName == "" {
			bookName = id
		}
		for _, c := range doc.Contacts {
			for _, n := range c.Telephony.Numbers {
				value := strings.TrimSpace(n.Value)
				if value == "" {
					continue
				}
				contacts = append(contacts, fbContact{
					Book: bookName, Name: strings.TrimSpace(c.Person.RealName),
					Number: value, Kind: n.Type,
				})
			}
		}
	}
	return contacts, nil
}

func newPhonebookCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "phonebook", Short: "Phonebook contacts stored on the router"}
	cmd.AddCommand(newPhonebookListCmd(flags), newPhonebookLookupCmd(flags))
	return cmd
}

func newPhonebookListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List every phonebook contact and number",
		Example:     "  fritzbox-pp-cli phonebook list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the phonebooks", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			contacts, err := box.Phonebook(ctx)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(contacts))
			for _, c := range contacts {
				rows = append(rows, map[string]any{"name": c.Name, "number": c.Number, "kind": c.Kind, "book": c.Book})
			}
			return fbEmit(cmd, flags, rows, "The router holds no phonebook entries.")
		},
	}
	return cmd
}

func newPhonebookLookupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "lookup <number-or-name>",
		Short:       "Find a phonebook entry by number or by name",
		Example:     "  fritzbox-pp-cli phonebook lookup 0301234567 --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "needle=1", "pp:no-error-path-probe": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "look up a phonebook entry", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a number or name is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			contacts, err := box.Phonebook(ctx)
			if err != nil {
				return err
			}
			needle := strings.ToLower(args[0])
			digits := onlyDigits(args[0])
			rows := make([]map[string]any, 0)
			for _, c := range contacts {
				// Numbers are compared digit-only so that spacing, dashes and
				// a country prefix do not defeat an otherwise exact match.
				if strings.Contains(strings.ToLower(c.Name), needle) ||
					(digits != "" && strings.Contains(onlyDigits(c.Number), digits)) {
					rows = append(rows, map[string]any{"name": c.Name, "number": c.Number, "kind": c.Kind, "book": c.Book})
				}
			}
			return fbEmit(cmd, flags, rows, "No phonebook entry matched.")
		},
	}
	return cmd
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ------------------------------------------------------------------- tam ----

func newTamToggleCmd(flags *rootFlags, enable bool) *cobra.Command {
	verb := "off"
	if enable {
		verb = "on"
	}
	var index int
	var confirm bool
	cmd := &cobra.Command{
		Use:     verb,
		Short:   fmt.Sprintf("Turn an answering machine %s", verb),
		Example: fmt.Sprintf("  fritzbox-pp-cli tam %s --index 0 --confirm", verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := fmt.Sprintf("turn answering machine %d %s", index, verb)
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, action, nil)
			}
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"index": index})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			value := "0"
			if enable {
				value = "1"
			}
			if _, err := box.Call(ctx, "X_AVM-DE_TAM1", "SetEnable", map[string]string{
				"Index": strconv.Itoa(index), "Enable": value,
			}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"index": index, "enabled": enable, "changed": true})
		},
	}
	cmd.Flags().IntVar(&index, "index", 0, "Which answering machine to act on")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func newTamMessagesCmd(flags *rootFlags) *cobra.Command {
	var index int
	cmd := &cobra.Command{
		Use:         "messages",
		Short:       "List the messages on an answering machine",
		Example:     "  fritzbox-pp-cli tam messages --index 0 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read answering-machine messages", map[string]any{"index": index})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, "X_AVM-DE_TAM1", "GetMessageList", map[string]string{"Index": strconv.Itoa(index)})
			if err != nil {
				return err
			}
			url := out["URL"]
			if url == "" {
				return apiErr(fmt.Errorf("router returned no message-list URL for answering machine %d", index))
			}
			// Same two-hop shape as the call journal: the SOAP response is a
			// pointer, not the data.
			body, err := box.web.Fetch(ctx, url)
			if err != nil {
				return fbErr(err)
			}
			var doc struct {
				Messages []struct {
					Index    string `xml:"Index"`
					Tam      string `xml:"Tam"`
					Called   string `xml:"Called"`
					Number   string `xml:"Number"`
					Name     string `xml:"Name"`
					New      string `xml:"New"`
					Date     string `xml:"Date"`
					Duration string `xml:"Duration"`
				} `xml:"Message"`
			}
			if err := xml.Unmarshal(body, &doc); err != nil {
				return apiErr(fmt.Errorf("parsing the answering-machine message list: %w", err))
			}
			rows := make([]map[string]any, 0, len(doc.Messages))
			for _, m := range doc.Messages {
				at := ""
				if t := parseFritzDate(m.Date); !t.IsZero() {
					at = t.Format(time.RFC3339)
				}
				rows = append(rows, map[string]any{
					"index": m.Index, "at": at, "name": strings.TrimSpace(m.Name),
					"number": strings.TrimSpace(m.Number), "duration": m.Duration, "unheard": m.New == "1",
				})
			}
			return fbEmit(cmd, flags, rows, "This answering machine has no messages.")
		},
	}
	cmd.Flags().IntVar(&index, "index", 0, "Which answering machine to read")
	return cmd
}

// ------------------------------------------------------------------ dect ----

func newDectSignalCmd(flags *rootFlags) *cobra.Command {
	var level int
	var confirm bool
	cmd := &cobra.Command{
		Use:   "signal",
		Short: "Read or reduce the DECT transmission power",
		Long: `Read the DECT base station's transmission power, or reduce it.

Lower power shrinks the range but reduces radio emissions, which is why FRITZ!OS
offers it as a comfort setting. Valid levels are 100, 50, 25, 12, and 6 percent.`,
		Example:     "  fritzbox-pp-cli dect signal --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or change the DECT transmission power", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if level == 0 {
				out, err := box.Call(ctx, "X_AVM-DE_Dect1", "GetNumberOfDectEntries", nil)
				if err != nil {
					return err
				}
				return fbEmitObject(cmd, flags, map[string]any{"handsets": out["NumberOfEntries"]})
			}
			switch level {
			case 100, 50, 25, 12, 6:
			default:
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--set must be one of 100, 50, 25, 12, or 6"))
			}
			if !confirm {
				return fbDryRun(cmd, flags, "change the DECT transmission power (pass --confirm to actually do it)", map[string]any{"level": level})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "change the DECT transmission power"); refused {
				return err
			}
			return apiErr(fmt.Errorf("this firmware exposes no TR-064 action for DECT transmission power; change it in the router's user interface under DECT, Base Station"))
		},
	}
	cmd.Flags().IntVar(&level, "set", 0, "Reduce transmission power to this percentage: 100, 50, 25, 12, or 6")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}
