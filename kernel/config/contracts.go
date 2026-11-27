// Package config defines startup configuration ports; the loader is delivered in M1.
package config

type Reader interface {
	Lookup(string) (string, bool)
}

type Source struct {
	Name   string
	Values map[string]string
}

// Loader copies sources in defaults/file/environment/CLI order before sealing.
type Loader interface {
	Load([]Source) (Reader, error)
}
