// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package fritzbox

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"fritzbox-pp-cli/internal/cliutil"
)

// TR064Port is the fixed port FRITZ!OS serves the TR-064 control interface on.
const TR064Port = 49000

// maxTR064Body caps a single SOAP response read. Service descriptors are the
// largest documents involved and stay well under a megabyte.
const maxTR064Body = 8 << 20

// Service is one entry from the router's TR-064 device description.
type Service struct {
	Type        string `json:"service_type" xml:"serviceType"`
	ID          string `json:"service_id" xml:"serviceId"`
	ControlURL  string `json:"control_url" xml:"controlURL"`
	SCPDURL     string `json:"scpd_url" xml:"SCPDURL"`
	EventSubURL string `json:"-" xml:"eventSubURL"`
}

// Name returns the short service name, for example "WLANConfiguration1" for
// "urn:dslforum-org:service:WLANConfiguration:1". This is the name the CLI
// accepts on the command line, because the full URN is unusable as an argument.
func (s Service) Name() string {
	parts := strings.Split(s.Type, ":")
	if len(parts) < 4 {
		return s.Type
	}
	return parts[len(parts)-2] + parts[len(parts)-1]
}

// Argument is one input or output of a TR-064 action.
type Argument struct {
	Name      string `json:"name" xml:"name"`
	Direction string `json:"direction" xml:"direction"`
	Variable  string `json:"state_variable" xml:"relatedStateVariable"`
	DataType  string `json:"data_type,omitempty" xml:"-"`
}

// Action is one callable operation on a service.
type Action struct {
	Name      string     `json:"name" xml:"name"`
	Arguments []Argument `json:"arguments" xml:"argumentList>argument"`
}

// In returns only the arguments the caller must supply.
func (a Action) In() []Argument { return a.filter("in") }

// Out returns only the arguments the router sends back.
func (a Action) Out() []Argument { return a.filter("out") }

func (a Action) filter(direction string) []Argument {
	var out []Argument
	for _, arg := range a.Arguments {
		if strings.EqualFold(arg.Direction, direction) {
			out = append(out, arg)
		}
	}
	return out
}

// scpd is the raw shape of a service control protocol description.
type scpd struct {
	Actions   []Action `xml:"actionList>action"`
	Variables []struct {
		Name     string `xml:"name"`
		DataType string `xml:"dataType"`
	} `xml:"serviceStateTable>stateVariable"`
}

// deviceDesc is the raw shape of tr64desc.xml. Services are nested one level
// deep under the root device, so a recursive device type is required.
type deviceDesc struct {
	Device device `xml:"device"`
}

type device struct {
	FriendlyName string    `xml:"friendlyName"`
	ModelName    string    `xml:"modelName"`
	Services     []Service `xml:"serviceList>service"`
	Devices      []device  `xml:"deviceList>device"`
}

// flatten walks the nested device tree and returns every service it declares.
func (d device) flatten() []Service {
	out := append([]Service(nil), d.Services...)
	for _, child := range d.Devices {
		out = append(out, child.flatten()...)
	}
	return out
}

// TR064 calls actions on a FRITZ!Box over SOAP with HTTP Digest auth.
type TR064 struct {
	base     string
	username string
	password string
	hc       *http.Client
	limiter  *cliutil.AdaptiveLimiter

	mu        sync.Mutex
	nonceSeq  int
	challenge *digestChallenge
}

// NewTR064 builds a client. base is the router origin, for example
// "http://fritz.box"; the TR-064 port is substituted automatically because the
// control interface never shares the web UI's port.
// NewTR064 builds the SOAP transport. limiter paces every request; pass nil to
// leave the transport unpaced, which only the manual smoke test wants.
func NewTR064(base, username, password string, hc *http.Client, limiter *cliutil.AdaptiveLimiter) *TR064 {
	return &TR064{base: tr064Origin(base), username: username, password: password, hc: hc, limiter: limiter}
}

// tr064Origin rewrites a router origin onto the TR-064 control port.
func tr064Origin(base string) string {
	trimmed := strings.TrimRight(base, "/")
	scheme := "http"
	rest := trimmed
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		scheme = trimmed[:idx]
		rest = trimmed[idx+3:]
	}
	// Drop any port already present; the web UI port is never the control port.
	if idx := strings.LastIndex(rest, ":"); idx >= 0 && !strings.Contains(rest[idx:], "]") {
		rest = rest[:idx]
	}
	// TR-064 is served over plain HTTP on 49000; TLS uses a different port and
	// a self-signed certificate, so the scheme is normalised down deliberately.
	if scheme == "https" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, rest, TR064Port)
}

// Describe fetches the device description and returns every declared service.
func (c *TR064) Describe(ctx context.Context) ([]Service, error) {
	body, err := c.get(ctx, "/tr64desc.xml")
	if err != nil {
		return nil, err
	}
	var desc deviceDesc
	if err := xml.Unmarshal(body, &desc); err != nil {
		return nil, fmt.Errorf("parsing the router service catalog: %w", err)
	}
	services := desc.Device.flatten()
	if len(services) == 0 {
		return nil, fmt.Errorf("router service catalog declared no services; is TR-064 enabled under Home Network, Network Settings, Allow access for applications?")
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name() < services[j].Name() })
	return services, nil
}

// Actions fetches and parses one service's control protocol description.
func (c *TR064) Actions(ctx context.Context, svc Service) ([]Action, error) {
	body, err := c.get(ctx, svc.SCPDURL)
	if err != nil {
		return nil, err
	}
	var doc scpd
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing the descriptor for service %s: %w", svc.Name(), err)
	}
	// The descriptor keeps argument types in a separate state table, so the
	// data type is resolved here rather than left for every caller to join.
	types := make(map[string]string, len(doc.Variables))
	for _, v := range doc.Variables {
		types[v.Name] = v.DataType
	}
	actions := doc.Actions
	for i := range actions {
		for j := range actions[i].Arguments {
			actions[i].Arguments[j].DataType = types[actions[i].Arguments[j].Variable]
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })
	return actions, nil
}

// SOAPFault reports a UPnP error returned inside an HTTP 500 response.
type SOAPFault struct {
	Code        string
	Description string
	Action      string
}

func (e *SOAPFault) Error() string {
	desc := e.Description
	if desc == "" {
		desc = "no description supplied"
	}
	return fmt.Sprintf("router rejected action %s: UPnP error %s (%s)", e.Action, e.Code, desc)
}

// Call invokes one action and returns its output arguments keyed by name with
// the "New" prefix FRITZ!OS uses stripped, so callers see "ExternalIPAddress"
// rather than "NewExternalIPAddress".
func (c *TR064) Call(ctx context.Context, svc Service, action string, args map[string]string) (map[string]string, error) {
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	body.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u=%q>`, action, svc.Type)
	// Sorted so a given call always produces a byte-identical body, which makes
	// the --dry-run preview stable and diffable.
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		wire := name
		if !strings.HasPrefix(wire, "New") {
			wire = "New" + wire
		}
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(args[name])); err != nil {
			return nil, fmt.Errorf("encoding argument %s: %w", name, err)
		}
		fmt.Fprintf(&body, "<%s>%s</%s>", wire, escaped.String(), wire)
	}
	fmt.Fprintf(&body, `</u:%s></s:Body></s:Envelope>`, action)

	resp, raw, err := c.post(ctx, svc.ControlURL, svc.Type+"#"+action, body.Bytes())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, parseSOAPFault(raw, action)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router returned HTTP %d for action %s", resp.StatusCode, action)
	}
	return decodeSOAPBody(raw)
}

// decodeSOAPBody pulls the response element's children out of the envelope.
// The element name is action-specific, so the document is streamed rather than
// unmarshalled into a fixed struct.
func decodeSOAPBody(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing the router SOAP response: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		depth++
		// Envelope > Body > <ActionResponse> > <NewFoo>: the values sit at the
		// fourth level, and only those are collected.
		if depth < 4 {
			continue
		}
		var value string
		if err := dec.DecodeElement(&value, &start); err != nil {
			return nil, fmt.Errorf("parsing SOAP element %s: %w", start.Name.Local, err)
		}
		depth--
		out[strings.TrimPrefix(start.Name.Local, "New")] = value
	}
	return out, nil
}

func parseSOAPFault(raw []byte, action string) error {
	var doc struct {
		Code string `xml:"Body>Fault>detail>UPnPError>errorCode"`
		Desc string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil || doc.Code == "" {
		return fmt.Errorf("router rejected action %s with an unparseable SOAP fault", action)
	}
	return &SOAPFault{Code: doc.Code, Description: doc.Desc, Action: action}
}

func (c *TR064) get(ctx context.Context, path string) ([]byte, error) {
	// Descriptor documents are public on FRITZ!OS, but the request still goes
	// through the digest-aware round-tripper so a locked-down box works too.
	resp, raw, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router returned HTTP %d for %s", resp.StatusCode, path)
	}
	return raw, nil
}

func (c *TR064) post(ctx context.Context, path, soapAction string, body []byte) (*http.Response, []byte, error) {
	return c.do(ctx, http.MethodPost, path, soapAction, body)
}

// do issues a request, answering a digest challenge when one comes back.
func (c *TR064) do(ctx context.Context, method, path, soapAction string, body []byte) (*http.Response, []byte, error) {
	resp, raw, err := c.attempt(ctx, method, path, soapAction, body, true)
	if err != nil {
		return nil, nil, err
	}
	// Checked ahead of the digest branch: a throttled box answers 429 or 503
	// without a WWW-Authenticate header, and treating that as a failed
	// challenge would report an auth problem the credentials do not have.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, nil, newThrottleError(resp, c.base+path)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, raw, nil
	}

	challenge := parseDigestChallenge(resp.Header.Get("WWW-Authenticate"))
	if challenge == nil {
		return nil, nil, fmt.Errorf("router demanded an authentication scheme this CLI does not implement: %q", resp.Header.Get("WWW-Authenticate"))
	}
	c.mu.Lock()
	c.challenge = challenge
	c.nonceSeq = 0
	c.mu.Unlock()

	resp, raw, err = c.attempt(ctx, method, path, soapAction, body, true)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil, fmt.Errorf("router rejected the TR-064 credentials; check FRITZBOX_USERNAME and FRITZBOX_PASSWORD, and confirm TR-064 is enabled under Home Network, Network Settings, Allow access for applications")
	}
	return resp, raw, nil
}

func (c *TR064) attempt(ctx context.Context, method, path, soapAction string, body []byte, withAuth bool) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, nil, err
	}
	if soapAction != "" {
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SoapAction", soapAction)
	}
	if withAuth {
		c.mu.Lock()
		challenge := c.challenge
		if challenge != nil {
			c.nonceSeq++
			seq := c.nonceSeq
			c.mu.Unlock()
			auth, authErr := challenge.authorization(c.username, c.password, method, path, seq)
			if authErr != nil {
				return nil, nil, authErr
			}
			req.Header.Set("Authorization", auth)
		} else {
			c.mu.Unlock()
		}
	}

	// Paced before the send, and the outcome is fed back below, so a box that
	// starts refusing slows every later caller down rather than only this one.
	c.limiter.Wait()
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("reaching the router TR-064 interface at %s: %w", c.base, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		c.limiter.OnRateLimit()
	} else {
		c.limiter.OnSuccess()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTR064Body))
	if err != nil {
		return nil, nil, fmt.Errorf("reading the router response: %w", err)
	}
	return resp, raw, nil
}
