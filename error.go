package wingman

import "fmt"

type Error struct {
	value interface{}
}

func NewError(val interface{}) Error {
	return Error{val}
}

func (e Error) Error() string {
	return fmt.Sprint(e.value)
}

type BackendIDError struct {
	ID  string
	Err error
}

func NewBackendIDError(id string, err error) BackendIDError {
	return BackendIDError{id, err}
}

func (e BackendIDError) Error() string {
	return e.Err.Error()
}
