package service

import "testing"

func TestValidateRemoteTransport(t *testing.T) {
	cases := []struct {
		name         string
		endpoint     string
		allowPrivate bool
		wantErr      bool
	}{
		{name: "https", endpoint: "https://example.com/v1", wantErr: false},
		{name: "loopback http", endpoint: "http://127.0.0.1:11434", wantErr: false},
		{name: "localhost http", endpoint: "http://localhost:8080", wantErr: false},
		{name: "ipv6 loopback http", endpoint: "http://[::1]:8080", wantErr: false},
		{name: "remote http", endpoint: "http://example.com/v1", wantErr: true},
		{name: "remote http private opt-in", endpoint: "http://example.com/v1", allowPrivate: true},
		{name: "invalid", endpoint: "not a url", wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := validateRemoteTransport(test.endpoint, test.allowPrivate)
			if test.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected", test.endpoint)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected %q to be accepted: %v", test.endpoint, err)
			}
		})
	}
}
