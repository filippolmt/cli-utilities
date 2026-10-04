package fritzbox

import (
	"errors"
	"strings"
	"testing"
)

func TestChallengeResponse(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
		password  string
		want      string
		wantErr   string
	}{
		{
			// Published AVM vector for the PBKDF2 scheme used by FRITZ!OS 7.24+.
			name:      "pbkdf2 published vector",
			challenge: "2$10000$5A1711$2000$5A1722",
			password:  "1example!",
			want:      "5A1722$1798a1672bca7c6463d6b245f82b53703b0f50813401b03e4045a5861e689adb",
		},
		{
			// Published AVM vector for the legacy MD5 scheme; the non-ASCII
			// password is the point, since it only matches under UTF-16LE.
			name:      "md5 legacy vector with non-ascii password",
			challenge: "1234567z",
			password:  "äbc",
			want:      "1234567z-9e224a41eeefa284df7bb0f26c2913e2",
		},
		{
			name:      "pbkdf2 challenge with too few fields",
			challenge: "2$10000$5A1711",
			password:  "x",
			wantErr:   "malformed PBKDF2 challenge",
		},
		{
			name:      "pbkdf2 challenge with non-numeric iterations",
			challenge: "2$abc$5A1711$2000$5A1722",
			password:  "x",
			wantErr:   "first iteration count",
		},
		{
			name:      "pbkdf2 challenge with non-hex salt",
			challenge: "2$10000$zz$2000$5A1722",
			password:  "x",
			wantErr:   "first salt",
		},
		{
			name:      "pbkdf2 challenge with zero iterations",
			challenge: "2$0$5A1711$2000$5A1722",
			password:  "x",
			wantErr:   "non-positive iteration count",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := challengeResponse(tt.challenge, tt.password)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDigestChallenge(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantNil   bool
		wantRealm string
		wantNonce string
		wantQop   string
	}{
		{
			name:      "fritzos shape",
			header:    `Digest realm="F!Box", nonce="ABC123", qop="auth"`,
			wantRealm: "F!Box",
			wantNonce: "ABC123",
			wantQop:   "auth",
		},
		{
			// A realm containing a comma must not split the parameter list.
			name:      "realm containing a comma",
			header:    `Digest realm="Home, Sweet Home", nonce="N1", qop="auth"`,
			wantRealm: "Home, Sweet Home",
			wantNonce: "N1",
			wantQop:   "auth",
		},
		{
			name:      "lowercase scheme token",
			header:    `digest realm="r", nonce="n"`,
			wantRealm: "r",
			wantNonce: "n",
		},
		{name: "basic scheme is not digest", header: `Basic realm="r"`, wantNil: true},
		{name: "digest without a nonce is unusable", header: `Digest realm="r"`, wantNil: true},
		{name: "empty header", header: "", wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDigestChallenge(tt.header)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("want nil challenge, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want a challenge, got nil")
			}
			if got.realm != tt.wantRealm {
				t.Errorf("realm: got %q, want %q", got.realm, tt.wantRealm)
			}
			if got.nonce != tt.wantNonce {
				t.Errorf("nonce: got %q, want %q", got.nonce, tt.wantNonce)
			}
			if got.qop != tt.wantQop {
				t.Errorf("qop: got %q, want %q", got.qop, tt.wantQop)
			}
		})
	}
}

func TestDigestAuthorization(t *testing.T) {
	c := &digestChallenge{realm: "F!Box", nonce: "N", qop: "auth"}
	got, err := c.authorization("user", "pw", "POST", "/upnp/control/deviceinfo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{`username="user"`, `realm="F!Box"`, `nonce="N"`, "qop=auth", "nc=00000001", "cnonce=", "response="} {
		if !strings.Contains(got, want) {
			t.Errorf("authorization header missing %q; got %s", want, got)
		}
	}

	// A password must never leak into the header itself.
	if strings.Contains(got, "pw") {
		t.Error("authorization header leaked the password")
	}

	if _, err := (&digestChallenge{nonce: "N", algorithm: "SHA-512"}).authorization("u", "p", "GET", "/", 1); err == nil {
		t.Error("want an error for an unsupported digest algorithm")
	}
}

func TestTR064Origin(t *testing.T) {
	tests := []struct{ in, want string }{
		{"http://fritz.box", "http://fritz.box:49000"},
		{"http://fritz.box/", "http://fritz.box:49000"},
		// The web UI's TLS port is not the control port, so https is normalised
		// down rather than carried over onto 49000.
		{"https://fritz.box", "http://fritz.box:49000"},
		{"http://192.168.178.1", "http://192.168.178.1:49000"},
		{"http://192.168.178.1:8080", "http://192.168.178.1:49000"},
		{"fritz.box", "http://fritz.box:49000"},
	}
	for _, tt := range tests {
		if got := tr064Origin(tt.in); got != tt.want {
			t.Errorf("tr064Origin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecodeSOAPBody(t *testing.T) {
	body := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:GetInfoResponse xmlns:u="urn:dslforum-org:service:DeviceInfo:1">
<NewModelName>FRITZ!Box 7590</NewModelName>
<NewUpTime>31626</NewUpTime>
<NewDescription>Release 154.08.25</NewDescription>
</u:GetInfoResponse></s:Body></s:Envelope>`
	got, err := decodeSOAPBody([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The "New" prefix is stripped so callers use the documented argument name.
	if got["ModelName"] != "FRITZ!Box 7590" {
		t.Errorf("ModelName: got %q", got["ModelName"])
	}
	if got["UpTime"] != "31626" {
		t.Errorf("UpTime: got %q", got["UpTime"])
	}
	if len(got) != 3 {
		t.Errorf("want 3 output arguments, got %d: %v", len(got), got)
	}
}

func TestParseSOAPFault(t *testing.T) {
	body := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault>
<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>
<detail><UPnPError xmlns="urn:dslforum-org:control-1-0">
<errorCode>713</errorCode><errorDescription>SpecifiedArrayIndexInvalid</errorDescription>
</UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	err := parseSOAPFault([]byte(body), "GetGenericEntry")
	var fault *SOAPFault
	if !asSOAPFault(err, &fault) {
		t.Fatalf("want a *SOAPFault, got %T: %v", err, err)
	}
	if fault.Code != "713" {
		t.Errorf("code: got %q, want 713", fault.Code)
	}
	if !strings.Contains(fault.Error(), "GetGenericEntry") {
		t.Errorf("error text should name the action: %s", fault.Error())
	}
}

func asSOAPFault(err error, target **SOAPFault) bool {
	return errors.As(err, target)
}

func TestServiceName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"urn:dslforum-org:service:WLANConfiguration:3", "WLANConfiguration3"},
		{"urn:dslforum-org:service:X_AVM-DE_OnTel:1", "X_AVM-DE_OnTel1"},
		{"not-a-urn", "not-a-urn"},
	}
	for _, tt := range tests {
		if got := (Service{Type: tt.in}).Name(); got != tt.want {
			t.Errorf("Service(%q).Name() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestActionInOut(t *testing.T) {
	a := Action{Arguments: []Argument{
		{Name: "NewIndex", Direction: "in"},
		{Name: "NewName", Direction: "out"},
		{Name: "NewActive", Direction: "OUT"},
	}}
	if len(a.In()) != 1 {
		t.Errorf("In(): got %d, want 1", len(a.In()))
	}
	// Direction casing varies between descriptors, so matching must be
	// case-insensitive or half the output arguments silently disappear.
	if len(a.Out()) != 2 {
		t.Errorf("Out(): got %d, want 2", len(a.Out()))
	}
}

func TestRedactSID(t *testing.T) {
	got := redactSID("http://192.168.178.1:49000/calllist.lua?sid=0123456789abcdef")
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("session id leaked into the redacted URL: %s", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("want a REDACTED marker, got %s", got)
	}
	if got := redactSID("://not a url"); got == "" {
		t.Error("an unparseable URL must still yield a safe placeholder")
	}
}

func TestBlockedErrorMessage(t *testing.T) {
	err := &BlockedError{Seconds: 30}
	if !strings.Contains(err.Error(), "30") {
		t.Errorf("blocked error should report the wait: %s", err.Error())
	}
}
