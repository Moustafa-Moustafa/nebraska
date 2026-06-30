package api

import "github.com/flatcar/nebraska/backend/pkg/api/internal/types"

// Arch types are owned by pkg/api/internal/types. The names are re-exported
// here so external callers continue to use api.Arch, api.ArchAMD64, etc.

type Arch = types.Arch

const (
	ArchAll     = types.ArchAll
	ArchAMD64   = types.ArchAMD64
	ArchAArch64 = types.ArchAArch64
	ArchX86     = types.ArchX86
)

// ErrInvalidArch is the same sentinel as types.ErrInvalidArch.
var ErrInvalidArch = types.ErrInvalidArch

// Constructor re-exports for callers that used api.ArchFromX(...).
var (
	ArchFromString       = types.ArchFromString
	ArchFromOmahaString  = types.ArchFromOmahaString
	ArchFromCoreosString = types.ArchFromCoreosString
)
