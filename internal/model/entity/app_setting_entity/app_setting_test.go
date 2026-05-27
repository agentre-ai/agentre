package app_setting_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsProxyKey(t *testing.T) {
	cases := []struct {
		name string
		s    *AppSetting
		want bool
	}{
		{"nil", nil, false},
		{"proxy host", &AppSetting{Key: KeyProxyListenHost}, true},
		{"proxy port", &AppSetting{Key: KeyProxyListenPort}, true},
		{"other proxy.*", &AppSetting{Key: "proxy.tls"}, true},
		{"unrelated", &AppSetting{Key: "theme.dark"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.s.IsProxyKey())
		})
	}
}

func TestValidateProxyHost(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"loopback", "127.0.0.1", false},
		{"any", "0.0.0.0", false},
		{"ipv6 loopback", "::1", false},
		{"with whitespace", "  127.0.0.1  ", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"hostname", "localhost", true},
		{"garbage", "not-an-ip", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProxyHost(ctx, tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateProxyPort(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"zero (random)", "0", false},
		{"valid", "60080", false},
		{"max", "65535", false},
		{"negative", "-1", true},
		{"overflow", "65536", true},
		{"non-numeric", "abc", true},
		{"empty", "", true},
		{"with whitespace", "  60080  ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProxyPort(ctx, tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseProxyPort(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"60080", 60080},
		{"65535", 65535},
		{"65536", 0},
		{"-1", 0},
		{"abc", 0},
		{"", 0},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseProxyPort(tc.input))
		})
	}
}
