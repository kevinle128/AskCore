package store

// Stores collects all store interfaces. The store implementation builds it,
// and internal/app gives each field to the code that needs it.
type Stores struct {
	Users UserStore
	Posts PostStore
}
