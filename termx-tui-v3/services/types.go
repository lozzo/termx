package services

// RequestID identifies an asynchronous service request.
type RequestID uint64

// Valid reports whether the request id can identify an in-flight request.
func (id RequestID) Valid() bool {
	return id != 0
}
