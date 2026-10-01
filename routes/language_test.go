package routes

import "testing"

func TestSupportedLanguage(t *testing.T) {
	tests := map[string]bool{
		"EN": true, "DE": true, "RS": true, "SR": true,
		"": false, "en": false, "FR": false, "EN'; --": false,
	}
	for lang, want := range tests {
		if got := supportedLanguage(lang); got != want {
			t.Errorf("supportedLanguage(%q) = %v, want %v", lang, got, want)
		}
	}
}
