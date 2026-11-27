// Package domain contains the Book aggregate and its repository port.
package domain

import (
	"context"
	"errors"
	"strings"
)

var ErrInvalidBook = errors.New("VALIDATION_FAILED")
var ErrNotFound = errors.New("NOT_FOUND")
var ErrConflict = errors.New("CONFLICT")

type Book struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}

func NewBook(id, title string) (Book, error) {
	title = strings.TrimSpace(title)
	if id == "" || title == "" || len(title) > 200 {
		return Book{}, ErrInvalidBook
	}
	return Book{ID: id, Title: title, Version: 1}, nil
}

type Repository interface {
	Insert(context.Context, Book) error
	Get(context.Context, string) (Book, error)
}

// VersionedRepository prevents lost updates and soft-deletes by expected version.
type VersionedRepository interface {
	Repository
	Update(context.Context, Book, int64) error
	Delete(context.Context, string, int64) error
}
