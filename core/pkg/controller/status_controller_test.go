package controller

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The front's certificate: both headers, well formed, or nothing.
func TestFrontCert(t *testing.T) {
	fp := strings.Repeat("ab:", 31) + "ab"
	pin := "sha256//" + strings.Repeat("A", 43) + "="
	for name, c := range map[string]struct {
		fp, pin string
		want    bool
	}{
		"both":           {fp, pin, true},
		"plain hex":      {strings.ReplaceAll(fp, ":", ""), pin, true},
		"none":           {"", "", false},
		"no pin":         {fp, "", false},
		"no fingerprint": {"", pin, false},
		"short":          {"ab:cd", pin, false},
		"not a pin":      {fp, "sha256//<script>", false},
	} {
		r := httptest.NewRequest("GET", "/status", nil)
		if c.fp != "" {
			r.Header.Set(HeaderFrontCertSHA256, c.fp)
		}
		if c.pin != "" {
			r.Header.Set(HeaderFrontCertPin, c.pin)
		}
		got := FrontCert(r)
		if (got != nil) != c.want {
			t.Errorf("%s: %+v", name, got)
		}
		if got != nil && (got.SHA256 != strings.ToUpper(fp) || got.Pin != pin) {
			t.Errorf("%s: %+v", name, got)
		}
	}
}
