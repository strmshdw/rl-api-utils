package statsapi

import (
	"context"
	"strings"
	"testing"
)

func TestEscapeXML(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Hello World", "Hello World"},
		{"Tom & Jerry", "Tom &amp; Jerry"},
		{"1 < 2 and 3 > 2", "1 &lt; 2 and 3 &gt; 2"},
		{"\"Quotes\" and 'Apos'", "&quot;Quotes&quot; and &apos;Apos&apos;"},
		{"Complex: <script>alert('XSS & more')</script>", "Complex: &lt;script&gt;alert(&apos;XSS &amp; more&apos;)&lt;/script&gt;"},
		{"$(calc.exe) `evil`", "$(calc.exe) `evil`"}, // Not special in XML, but kept intact safely
	}

	for _, c := range cases {
		out := escapeXML(c.input)
		if out != c.expected {
			t.Errorf("escapeXML(%q) = %q; expected %q", c.input, out, c.expected)
		}
	}
}

func TestNoopToastNotifier(t *testing.T) {
	n := &NoopToastNotifier{}
	ctx := context.Background()

	if err := n.ShowToast(ctx, "Title 1", "Message 1"); err != nil {
		t.Fatalf("ShowToast failed: %v", err)
	}
	if err := n.ShowToast(ctx, "Title 2", "Message 2"); err != nil {
		t.Fatalf("ShowToast failed: %v", err)
	}

	n.mu.Lock()
	count := len(n.Notifications)
	n.mu.Unlock()

	if count != 2 {
		t.Fatalf("expected 2 notifications; got %d", count)
	}
}

func TestWindowsToastNotifier_XMLStructure(t *testing.T) {
	// Verify that escapeXML properly creates valid XML payloads for hostile inputs
	hostileTitle := "Tom & Jerry <test> 'quotes' \"double\""
	hostileMessage := "$(Start-Process calc) `powershell injection` & <tags>"

	payload := "<toast><visual><binding template='ToastGeneric'><text>" +
		escapeXML(hostileTitle) + "</text><text>" +
		escapeXML(hostileMessage) + "</text></binding></visual></toast>"

	if strings.Contains(payload, "<test>") {
		t.Errorf("unquoted tag found in payload: %s", payload)
	}
	if strings.Contains(payload, "Tom & Jerry") {
		t.Errorf("unescaped ampersand found in payload: %s", payload)
	}
	if !strings.Contains(payload, "Tom &amp; Jerry") {
		t.Errorf("expected escaped ampersand: %s", payload)
	}
}
