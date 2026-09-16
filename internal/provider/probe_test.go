package provider

import (
	"net/http"
	"strings"
	"testing"
)

func TestInterpretProbe(t *testing.T) {
	base := Reachability{Site: "global", Endpoint: "https://www.codebuddy.ai/v2/chat/completions"}
	up := InterpretProbe(base, http.StatusUnauthorized, []byte("Authorization Required"), nil)
	if !up.OK || up.Cause != "reachable" {
		t.Fatalf("401 should mean auth layer up: %+v", up)
	}
	down := InterpretProbe(base, http.StatusBadGateway, []byte("<html>openresty"), nil)
	if down.OK || down.Cause != "upstream_infra" {
		t.Fatalf("502 HTML should be infra down: %+v", down)
	}
	gw := InterpretProbe(base, http.StatusGatewayTimeout, []byte("timeout"), nil)
	if gw.OK || gw.Cause != "upstream_infra" {
		t.Fatalf("504 should be infra down: %+v", gw)
	}
}

func TestChatErrorRetryAfter(t *testing.T) {
	err := &ChatError{Status: 429, RetryAfter: "12", Msg: "failed with 429"}
	if err.Error() != "failed with 429" {
		t.Fatalf("Error()=%q", err.Error())
	}
	if got := err.RetryAfterHeader(); got != "12" {
		t.Fatalf("RetryAfter=%q", got)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatal("retry regex still needs 429 in Error()")
	}
}
