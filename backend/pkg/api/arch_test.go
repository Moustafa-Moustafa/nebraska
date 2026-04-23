package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestArch(t *testing.T) {
	for _, tt := range []struct {
		arch   api.Arch
		valid  bool
		our    string
		omaha  string
		coreos string
	}{
		{
			arch:   api.ArchAll,
			valid:  true,
			our:    "all",
			omaha:  "",
			coreos: "",
		},
		{
			arch:   api.ArchAMD64,
			valid:  true,
			our:    "amd64",
			omaha:  "x64",
			coreos: "amd64-usr",
		},
		{
			arch:   api.ArchX86,
			valid:  true,
			our:    "x86",
			omaha:  "x86",
			coreos: "",
		},
		{
			arch:   api.ArchAArch64,
			valid:  true,
			our:    "aarch64",
			omaha:  "arm",
			coreos: "arm64-usr",
		},
		{
			arch:   api.Arch(77777),
			valid:  false,
			our:    "Arch(77777)",
			omaha:  "",
			coreos: "",
		},
	} {
		assert.Equal(t, tt.valid, tt.arch.IsValid())
		assert.Equal(t, tt.our, tt.arch.String())
		assert.Equal(t, tt.omaha, tt.arch.OmahaString())
		assert.Equal(t, tt.coreos, tt.arch.CoreosString())
		gotOur, errOur := api.ArchFromString(tt.our)
		gotOmaha, errOmaha := api.ArchFromOmahaString(tt.omaha)
		gotCoreos, errCoreos := api.ArchFromCoreosString(tt.coreos)
		if !tt.valid {
			assert.Equal(t, api.ErrInvalidArch, errOur)
		} else {
			assert.Equal(t, tt.arch, gotOur)
			assert.NoError(t, errOur)
		}
		if !tt.valid || tt.omaha == "" {
			assert.Equal(t, api.ErrInvalidArch, errOmaha)
		} else {
			assert.Equal(t, tt.arch, gotOmaha)
			assert.NoError(t, errOmaha)
		}
		if !tt.valid || tt.coreos == "" {
			assert.Equal(t, api.ErrInvalidArch, errCoreos)
		} else {
			assert.Equal(t, tt.arch, gotCoreos)
			assert.NoError(t, errCoreos)
		}
	}
}
