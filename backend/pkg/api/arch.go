package api

import "github.com/flatcar/nebraska/backend/pkg/api/internal/types"

type Arch = types.Arch

const (
	ArchAll     = types.ArchAll
	ArchAMD64   = types.ArchAMD64
	ArchAArch64 = types.ArchAArch64
	ArchX86     = types.ArchX86
)

var ErrInvalidArch = types.ErrInvalidArch

var (
	ArchFromString       = types.ArchFromString
	ArchFromOmahaString  = types.ArchFromOmahaString
	ArchFromCoreosString = types.ArchFromCoreosString
)
