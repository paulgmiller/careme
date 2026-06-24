package geo

import "testing"

func TestTimezoneNameForZip(t *testing.T) {
	cases := []struct {
		zip      string
		wantName string
		wantOK   bool
	}{
		{zip: "10001", wantName: "America/New_York", wantOK: true},
		{zip: "60601", wantName: "America/Chicago", wantOK: true},
		{zip: "80202", wantName: "America/Denver", wantOK: true},
		{zip: "94105", wantName: "America/Los_Angeles", wantOK: true},
		{zip: "abcde", wantName: "", wantOK: false},
	}
	for _, tc := range cases {
		gotName, gotOK := TimezoneNameForZip(tc.zip)
		if gotName != tc.wantName || gotOK != tc.wantOK {
			t.Fatalf("zip %q: got (%q,%t), want (%q,%t)", tc.zip, gotName, gotOK, tc.wantName, tc.wantOK)
		}
	}
}
