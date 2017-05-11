package discovery

// Source defines the interface for collecting targets from various
// services. New services should implement this interface.
type Source interface {
	// Collect retrieves all targets from a source.
	Collect() error

	// Save writes the targets to the named file.
	Save() error
}

// Factory defines the interface for creating new Source instances.
type Factory interface {
	// Create creates a new Source ready for collection.
	Create() (Source, error)
}
