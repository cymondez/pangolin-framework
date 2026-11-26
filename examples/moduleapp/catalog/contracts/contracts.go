// Package contracts publishes the catalog service contract. The catalog
// module owns this contract; consumers depend on the catalog module and
// resolve the contract from their restricted root resolver. Implementations
// from other modules contribute only by an explicit association declared in
// the implementing package; structural satisfaction alone never exposes one.
package contracts

// Catalog is the public catalog read/write contract demonstrated by
// examples/moduleapp. The concrete implementation is registered by the
// catalog module with ordinary constructor registration and exported to the
// contract.
type Catalog interface {
	Put(id, title string) error
	Get(id string) (Item, bool)
	Len() int
}

// Item is a catalog entry value. Data contracts are plain values; callers
// still run inside their own authorized context.
type Item struct {
	ID    string
	Title string
}
