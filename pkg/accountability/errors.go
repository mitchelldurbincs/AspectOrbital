package accountability

import "errors"

var (
	ErrInvalid  = errors.New("invalid accountability request")
	ErrNotFound = errors.New("accountability record not found")
	ErrConflict = errors.New("accountability conflict")
)

type classifiedError struct {
	kind  error
	msg   string
	cause error
}

func (e *classifiedError) Error() string {
	if e == nil {
		return ""
	}
	if e.msg != "" {
		return e.msg
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	if e.kind != nil {
		return e.kind.Error()
	}
	return ""
}

func (e *classifiedError) Unwrap() []error {
	errors := make([]error, 0, 2)
	if e.kind != nil {
		errors = append(errors, e.kind)
	}
	if e.cause != nil {
		errors = append(errors, e.cause)
	}
	return errors
}

func invalidError(msg string) error {
	return &classifiedError{kind: ErrInvalid, msg: msg}
}

func notFoundError(msg string) error {
	return &classifiedError{kind: ErrNotFound, msg: msg}
}

func conflictError(msg string) error {
	return &classifiedError{kind: ErrConflict, msg: msg}
}

func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var classified *classifiedError
	if errors.As(err, &classified) {
		return classified.Error()
	}
	return err.Error()
}
