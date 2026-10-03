package auth

import (
	"strings"
	"testing"
)

func TestErrorPageEscapesMessage(t *testing.T) {
	message := `Missing </p><script>alert("callback")</script> & token`
	page := errorPage(message)
	if strings.Contains(page, message) || strings.Contains(page, "<script>") {
		t.Fatal("callback error message was rendered as HTML")
	}
	if !strings.Contains(page, "Missing &lt;/p&gt;&lt;script&gt;") || !strings.Contains(page, "&amp; token") {
		t.Fatal("escaped callback error message is missing")
	}
}
