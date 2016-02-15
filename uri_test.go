package turms

import (
	"testing"
)

var (
	validURIs = []URI{
		"1234567890",
		"013",
		"xyz._._",
		"wamp.error.not_authorized",
		"wamp.error.procedure_already_exists",
	}
	invalidURIs = []URI{
		"xyz._.",
		"##",
		"wamp.error..not_authorized",
		"com.my\n",
	}
)

func TestURI(t *testing.T) {
	for _, u := range validURIs {
		if !u.Valid() {
			t.Errorf("Expected %s to be valid, got invalid", u)
		}
	}

	for _, u := range invalidURIs {
		if u.Valid() {
			t.Errorf("Expected %s to be invalid, got valid", u)
		}
	}
}
