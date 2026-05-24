package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCallbackURL_FullURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		state       string
		wantCode    string
		wantState   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid full URL",
			url:       "http://127.0.0.1:12345/?code=abc123&state=mystate",
			state:     "mystate",
			wantCode:  "abc123",
			wantState: "mystate",
		},
		{
			name:      "valid HTTPS URL",
			url:       "https://localhost/callback?code=xyz789&state=s2",
			state:     "s2",
			wantCode:  "xyz789",
			wantState: "s2",
		},
		{
			name:      "code only (no state check)",
			url:       "http://localhost/?code=only-code",
			state:     "",
			wantCode:  "only-code",
			wantState: "",
		},
		{
			name:        "missing code",
			url:         "http://localhost/?state=test",
			state:       "test",
			wantErr:     true,
			errContains: "no authorization code",
		},
		{
			name:        "OAuth error",
			url:         "http://localhost/?error=access_denied&state=test",
			state:       "test",
			wantErr:     true,
			errContains: "OAuth error",
		},
		{
			name:        "state mismatch",
			url:         "http://localhost/?code=abc&state=wrong",
			state:       "expected",
			wantErr:     true,
			errContains: "state mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCallbackURL(tt.url, tt.state)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCallbackURL() error = nil, want error containing %q", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ParseCallbackURL() error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCallbackURL() unexpected error = %v", err)
			}
			if result.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", result.Code, tt.wantCode)
			}
			if result.State != tt.wantState {
				t.Errorf("State = %q, want %q", result.State, tt.wantState)
			}
		})
	}
}

func TestParseCallbackURL_QueryOnly(t *testing.T) {
	// User might paste just the query string
	result, err := ParseCallbackURL("code=mycode&state=mystate", "mystate")
	if err != nil {
		t.Fatalf("ParseCallbackURL() error = %v", err)
	}
	if result.Code != "mycode" {
		t.Errorf("Code = %q, want %q", result.Code, "mycode")
	}
}

func TestParseCallbackURL_QueryWithQuestionMark(t *testing.T) {
	result, err := ParseCallbackURL("?code=mycode&state=mystate", "mystate")
	if err != nil {
		t.Fatalf("ParseCallbackURL() error = %v", err)
	}
	if result.Code != "mycode" {
		t.Errorf("Code = %q, want %q", result.Code, "mycode")
	}
}

func TestParseCallbackURL_JustCode(t *testing.T) {
	// User might paste just the code
	result, err := ParseCallbackURL("4/0AfJohXkABC123...", "")
	if err != nil {
		t.Fatalf("ParseCallbackURL() error = %v", err)
	}
	if result.Code != "4/0AfJohXkABC123..." {
		t.Errorf("Code = %q, want %q", result.Code, "4/0AfJohXkABC123...")
	}
}

func TestManualCodeInput(t *testing.T) {
	input := strings.NewReader("http://localhost/?code=manual-code&state=manual-state\n")
	output := &bytes.Buffer{}

	result, err := ManualCodeInput(input, output, "manual-state")
	if err != nil {
		t.Fatalf("ManualCodeInput() error = %v", err)
	}

	if result.Code != "manual-code" {
		t.Errorf("Code = %q, want %q", result.Code, "manual-code")
	}

	// Check that instructions were printed
	if !strings.Contains(output.String(), "paste it here") {
		t.Errorf("output missing instructions: %s", output.String())
	}
}

func TestManualCodeInput_Empty(t *testing.T) {
	input := strings.NewReader("\n")
	output := &bytes.Buffer{}

	_, err := ManualCodeInput(input, output, "state")
	if err == nil {
		t.Error("ManualCodeInput() expected error for empty input")
	}
}

func TestManualCodeInput_EOF(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}

	_, err := ManualCodeInput(input, output, "state")
	if err == nil {
		t.Error("ManualCodeInput() expected error for EOF")
	}
}
