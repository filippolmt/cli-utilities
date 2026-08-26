package fritzbox

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveSmoke exercises the real device. Skipped unless FRITZBOX_LIVE=1.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("FRITZBOX_LIVE") != "1" {
		t.Skip("set FRITZBOX_LIVE=1 to run against a real router")
	}
	base := "http://fritz.box"
	hc := &http.Client{Timeout: 15 * time.Second}
	ctx := context.Background()

	c := NewTR064(base, os.Getenv("FRITZBOX_USERNAME"), os.Getenv("FRITZBOX_PASSWORD"), hc, NewLimiter(0))
	svcs, err := c.Describe(ctx)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	t.Logf("services=%d", len(svcs))

	var info Service
	for _, s := range svcs {
		if s.Name() == "DeviceInfo1" {
			info = s
		}
	}
	if info.Type == "" {
		t.Fatal("DeviceInfo1 not found")
	}
	acts, err := c.Actions(ctx, info)
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	t.Logf("DeviceInfo1 actions=%d", len(acts))

	out, err := c.Call(ctx, info, "GetInfo", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	t.Logf("model=%q swver=%q uptime=%q", out["ModelName"], out["SoftwareVersion"], out["UpTime"])

	res, err := Login(ctx, hc, base, os.Getenv("FRITZBOX_USERNAME"), os.Getenv("FRITZBOX_PASSWORD"), NewLimiter(0))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	t.Logf("sid_len=%d rights=%d", len(res.SID), len(res.Rights))
}
