package client

// Category groups states by semantic meaning. Names are user-facing labels;
// categories are what the rest of the system reasons about (e.g. filtering
// open work, deciding dependency readiness, coloring rows).
type Category int

const (
	CategoryOpen      Category = iota // not started yet
	CategoryActive                    // in flight
	CategoryDone                      // finished successfully
	CategoryCancelled                 // finished without completion
)
