package status

import (
	"fmt"

	"github.com/breez/lspd/lsps0/codes"
)

type Status struct {
	Code    codes.Code
	Message string
}

func New(c codes.Code, msg string) *Status {
	return &Status{Code: c, Message: msg}
}

func Newf(c codes.Code, format string, a ...interface{}) *Status {
	return New(c, fmt.Sprintf(format, a...))
}

func FromError(err error) (s *Status, ok bool) {
	if err == nil {
		return nil, true
	}
	if se, ok := err.(interface {
		Lsps0Status() *Status
	}); ok {
		return se.Lsps0Status(), true
	}
	return New(codes.Unknown, err.Error()), false
}

// Convert is a convenience function which removes the need to handle the
// boolean return value from FromError.
func Convert(err error) *Status {
	s, _ := FromError(err)
	return s
}

func (s *Status) Err() error {
	if s.Code == codes.OK {
		return nil
	}
	return &Error{s: s}
}

func (s *Status) String() string {
	return fmt.Sprintf("lsps0 error: code = %d desc = %s", int32(s.Code), s.Message)
}

type Error struct {
	s *Status
}

func (e *Error) Error() string {
	return e.s.String()
}

func (e *Error) Lsps0Status() *Status {
	return e.s
}
