package whatsapp

import "testing"

func TestParseJID(t *testing.T) {
	device := &Device{}

	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantUser   string
		wantServer string
	}{
		{name: "plain international", input: "919876543210", wantOK: true, wantUser: "919876543210", wantServer: "s.whatsapp.net"},
		{name: "plus prefix", input: "+919876543210", wantOK: true, wantUser: "919876543210", wantServer: "s.whatsapp.net"},
		{name: "whitespace", input: " 919876543210 ", wantOK: true, wantUser: "919876543210", wantServer: "s.whatsapp.net"},
		{name: "explicit server", input: "919876543210@s.whatsapp.net", wantOK: true, wantUser: "919876543210", wantServer: "s.whatsapp.net"},
		{name: "empty", input: "", wantOK: false},
		{name: "only plus", input: "+", wantOK: false},
		{name: "letters", input: "abc123", wantOK: false},
		{name: "dashes", input: "91987-6543210", wantOK: false},
		{name: "missing user", input: "@s.whatsapp.net", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jid, ok := device.ParseJID(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ParseJID(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if jid.User != tt.wantUser {
				t.Errorf("ParseJID(%q) user = %q, want %q", tt.input, jid.User, tt.wantUser)
			}
			if jid.Server != tt.wantServer {
				t.Errorf("ParseJID(%q) server = %q, want %q", tt.input, jid.Server, tt.wantServer)
			}
		})
	}
}

func TestLogLevelDefault(t *testing.T) {
	t.Setenv("WHATSAPP_LOG_LEVEL", "")
	if got := logLevel(); got != "INFO" {
		t.Fatalf("logLevel() = %q, want INFO", got)
	}
}

func TestLogLevelAccepted(t *testing.T) {
	t.Setenv("WHATSAPP_LOG_LEVEL", "debug")
	if got := logLevel(); got != "DEBUG" {
		t.Fatalf("logLevel() = %q, want DEBUG", got)
	}
}

func TestLogLevelRejectsUnknown(t *testing.T) {
	t.Setenv("WHATSAPP_LOG_LEVEL", "verbose")
	if got := logLevel(); got != "INFO" {
		t.Fatalf("logLevel() = %q, want INFO", got)
	}
}
