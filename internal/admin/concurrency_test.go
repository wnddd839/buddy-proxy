package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// paintConcurrencySource extracts the admin console function so the test
// exercises the shipped JavaScript rather than a copy of it.
func paintConcurrencySource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("page.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "function paintConcurrency(c){")
	if start < 0 {
		t.Fatal("paintConcurrency not found")
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	t.Fatal("unterminated paintConcurrency")
	return ""
}

// TestPaintConcurrencyAcceptsBothShapes runs the shipped function under Node
// against /api/status (numeric summary) and /api/concurrency (per-account
// snapshot). A regression that calls .forEach on the numeric summary throws
// and aborts paintStatus, leaving the whole account pool panel empty.
func TestPaintConcurrencyAcceptsBothShapes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	script := paintConcurrencySource(t) + `
const out = {};
const $ = (id) => {
  if (!out[id]) out[id] = { textContent: '', style: {} };
  return out[id];
};
paintConcurrency({ generatedAt: 1, globalInFlight: 7, accounts: 5, rateLimits: 8 });
if (out.ccInFlight.textContent !== '7' || out.ccPeak.textContent !== '0' || out.ccLimited.textContent !== '8') {
  console.error('numeric summary:', JSON.stringify(out));
  process.exit(1);
}
paintConcurrency({
  generatedAt: 2, globalInFlight: 4, rateLimits: [],
  accounts: [
    { accountId: 'a', peakInFlight: 13, rateLimits60: 2 },
    { accountId: 'b', peakInFlight: 3, rateLimits60: 1 }
  ]
});
if (out.ccInFlight.textContent !== '4' || out.ccPeak.textContent !== '13' || out.ccLimited.textContent !== '3') {
  console.error('snapshot:', JSON.stringify(out));
  process.exit(1);
}
console.log('ok');
`
	dir := t.TempDir()
	path := filepath.Join(dir, "check.js")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("paintConcurrency failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "ok") {
		t.Fatalf("unexpected output: %s", output)
	}
}
