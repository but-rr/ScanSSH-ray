package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRanges(t *testing.T) {
	_, ips, err := Expand("10.0.113.1-30/24,10.0.113.100-130/24\n10.32.237.1-30/24")
	if err != nil || len(ips) != 91 {
		t.Fatalf("prototype ranges: count=%d err=%v", len(ips), err)
	}
	_, ips, err = Expand("192.168.1.1-3,192.168.1.2,192.168.2.0/30")
	if err != nil || len(ips) != 5 {
		t.Fatalf("dedup/CIDR: %v %v", ips, err)
	}
	for _, bad := range []string{"", "10.0.0.9-2", "999.1.1.1", "10.0.0.0/8", "10.0.0.1-65535", "10.0.0.1-2/0", "10.0.0.1;whoami"} {
		if _, _, err := Expand(bad); err == nil {
			t.Errorf("accepted invalid range %q", bad)
		}
	}
	if !Match("10.0.113.*", "10.0.113.255") || Match("10.0.113.*", "10.0.114.1") {
		t.Fatal("wildcard matching")
	}
}

func TestConfigDoesNotExposeSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	text := `SCAN_RANGES='127.0.0.1'
SSH_KEY_PATH='keys/default'
SSH_KEY_PASSPHRASE='private-passphrase-unique'
PROXY_USER='private-user-unique'
PROXY_PASSWORD='private-password-unique'
SSH_KEY_RULES='[{"match":"127.0.0.1","path":"keys/specific","user":"operator","passphrase":"rule-secret-unique"}]'
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key, phrase, user := c.Identity("127.0.0.1")
	if !filepath.IsAbs(key) || !strings.HasSuffix(key, "specific") || phrase != "rule-secret-unique" || user != "operator" {
		t.Fatal("key rule resolution")
	}
	public, _ := json.Marshal(c.Summary())
	for _, secret := range []string{"private-passphrase-unique", "private-user-unique", "private-password-unique", "rule-secret-unique"} {
		if strings.Contains(string(public), secret) {
			t.Fatalf("public config leaked %s", secret)
		}
	}
}

func TestConfigRejectsInvalidValuesWithoutEchoingSecrets(t *testing.T) {
	for _, line := range []string{"SSH_KEY_RULES='secret-invalid-json'", "SSH_PORT=0", "AUTO_SCAN=nope", "PROXY_AUTH=ntlm", "TEST_URL='ftp://example.com'", "TEST_URL='http://user:secret@example.com'", "SCAN_CONCURRENCY=999"} {
		path := filepath.Join(t.TempDir(), ".env")
		_ = os.WriteFile(path, []byte(line), 0600)
		_, err := Load(path)
		if err == nil {
			t.Errorf("accepted %s", line)
		} else if strings.Contains(err.Error(), "secret-invalid-json") {
			t.Fatal("error leaked value")
		}
	}
}

func TestProbeURLsAndLegacyMigration(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"", DefaultTestURL},
		{"TEST_URL='" + legacyTestURL + "'", DefaultTestURL},
		{"TEST_URL='http://example.com'", "http://example.com"},
		{"TEST_URL='https://example.com'", "https://example.com"},
	} {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte(test.input), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if c.TestURL != test.want {
			t.Fatalf("URL %q: got %q, want %q", test.input, c.TestURL, test.want)
		}
		unchanged, err := os.ReadFile(path)
		if err != nil || string(unchanged) != test.input {
			t.Fatal("loading configuration changed the user's .env")
		}
	}
}
