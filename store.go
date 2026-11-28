package imap

// StoreOptions contains options for the STORE command.
type StoreOptions struct {
	// requires CONDSTORE
	// UnchangedSince=0 is valid and is different from missing UNCHANGEDSINCE - see RFC 7162 3.1.3 Examples 6, 7
	UnchangedSince *uint64
}

// StoreFlagsOp is a flag operation: set, add or delete.
type StoreFlagsOp int

const (
	StoreFlagsSet StoreFlagsOp = iota
	StoreFlagsAdd
	StoreFlagsDel
)

// StoreFlags alters message flags.
type StoreFlags struct {
	Op     StoreFlagsOp
	Silent bool
	Flags  []Flag
}

// StoreData is additional information returned by the STORE command.
type StoreData struct {
	Modified NumSet
}
