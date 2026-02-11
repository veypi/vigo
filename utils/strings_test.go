package utils

import (
	"testing"
)

func TestCamelToSnake(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"CamelToSnake", "camel_to_snake"},
		{"APIKey", "api_key"},
		{"AbcID", "abc_id"},
		{"caseID", "case_id"},
		{"ABCDef", "abc_def"},
		{"simple", "simple"},
		{"Simple", "simple"},
		{"HTTPSConnection", "https_connection"},
		{"getHTTPResponse", "get_http_response"},
		{"XMLParser", "xml_parser"},
		{"JSONData", "json_data"},
		{"aBC", "a_bc"},
		{"ABC", "abc"},
		{"", ""},
	}

	for _, tc := range testCases {
		result := CamelToSnake(tc.input)
		if result != tc.expected {
			t.Errorf("CamelToSnake(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}
