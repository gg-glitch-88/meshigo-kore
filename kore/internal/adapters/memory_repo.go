package adapters

import (
	"context"
	"errors"
)

// InMemoryRepo is a concrete struct.
// Note: It does NOT say "implements DataProvider".
// It just *happens* to satisfy the interface. This is Duck Typing.
type InMemoryRepo struct {
	store map[string]string
}

func NewInMemoryRepo() *InMemoryRepo {
	return &InMemoryRepo{
		store: make(map[string]string),
	}
}

func (r *InMemoryRepo) FetchData(ctx context.Context, id string) (string, error) {
	val, ok := r.store[id]
	if !ok {
		return "", errors.New("not found")
	}
	return val, nil
}