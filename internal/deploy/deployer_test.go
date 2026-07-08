package deploy

import "testing"

func TestValidateAppName(t *testing.T) {
	valid := []string{"a", "my-app", "app2", "0leading-digit", "exactly-39-chars-abcdefghijklmnopqrstuv"}
	for _, name := range valid {
		if err := ValidateAppName(name); err != nil {
			t.Errorf("ValidateAppName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "-leading-dash", "UPPER", "under_score", "dot.name", "app name",
		"way-too-long-name-that-exceeds-the-forty-char-limit"}
	for _, name := range invalid {
		if err := ValidateAppName(name); err == nil {
			t.Errorf("ValidateAppName(%q) = nil, want error", name)
		}
	}
}

func TestURL(t *testing.T) {
	if got := NewDeployer(nil, "localtest.me", 80).URL("demo"); got != "http://demo.localtest.me" {
		t.Errorf("URL on port 80 = %q", got)
	}
	if got := NewDeployer(nil, "localtest.me", 8080).URL("demo"); got != "http://demo.localtest.me:8080" {
		t.Errorf("URL on port 8080 = %q", got)
	}
}

func TestNamespace(t *testing.T) {
	if got := Namespace("demo"); got != "minato-app-demo" {
		t.Errorf("Namespace(demo) = %q", got)
	}
}
