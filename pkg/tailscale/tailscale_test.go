package tailscale

import "testing"

func TestStatCommandNotFound(t *testing.T) {
	_, err := Stat("cloudkey-no-such-binary")
	if err == nil {
		t.Fatal("expected error for missing tailscale binary")
	}
}

func TestParseStatusUp(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"running is up", `{"BackendState":"Running"}`, true},
		{"needs login is down", `{"BackendState":"NeedsLogin"}`, false},
		{"stopped is down", `{"BackendState":"Stopped"}`, false},
		{"missing field is down", `{}`, false},
		{"malformed json is down", `not json`, false},
		{"empty output is down", ``, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseStatus([]byte(c.out)).Up; got != c.want {
				t.Errorf("parseStatus(%q).Up = %v, want %v", c.out, got, c.want)
			}
		})
	}
}

func TestParseStatusTransferAndPingAddr(t *testing.T) {
	const out = `{
	  "BackendState": "Running",
	  "Peer": {
	    "keyA": { "TailscaleIPs": ["100.64.0.2", "fd7a:1::2"], "RxBytes": 1000, "TxBytes": 500, "Online": false },
	    "keyB": { "TailscaleIPs": ["100.64.0.3", "fd7a:1::3"], "RxBytes": 2000, "TxBytes": 250, "Online": true }
	  }
	}`

	st := parseStatus([]byte(out))
	if !st.Up {
		t.Errorf("Up = false, want true")
	}
	if st.Rx != 3000 || st.Tx != 750 {
		t.Errorf("transfer = (%d, %d), want (3000, 750)", st.Rx, st.Tx)
	}
	// PingAddr must be an online peer's IPv4, never the offline peer's or an
	// IPv6.
	if st.PingAddr != "100.64.0.3" {
		t.Errorf("PingAddr = %q, want 100.64.0.3", st.PingAddr)
	}
}

func TestParseStatusNoOnlinePeerHasNoPingAddr(t *testing.T) {
	const out = `{
	  "BackendState": "Running",
	  "Peer": {
	    "keyA": { "TailscaleIPs": ["100.64.0.2"], "RxBytes": 10, "TxBytes": 20, "Online": false }
	  }
	}`

	if got := parseStatus([]byte(out)).PingAddr; got != "" {
		t.Errorf("PingAddr = %q, want empty", got)
	}
}
