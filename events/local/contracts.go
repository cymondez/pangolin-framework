// Package local defines the in-UOW event queue port.
package local

import "context"

type LocalEventBus interface {
	Enqueue(context.Context, any) error
}
