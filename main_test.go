package main

import "testing"

func TestGenerateShortCode(t *testing.T) {
	code := generateShortCode(6)

	if len(code) != 6 {
		t.Errorf("expected length 6, got %d", len(code))
	}

	for _, c := range code {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("unexpected character in short code: %c", c)
		}
	}
}

func TestIsValidURL(t *testing.T) {
	valid := "https://www.google.com"
	invalid := "banana"

	if !isValidURL(valid) {
		t.Errorf("expected %s to be valid", valid)
	}

	if isValidURL(invalid) {
		t.Errorf("expected %s to be invalid", invalid)
	}
}
