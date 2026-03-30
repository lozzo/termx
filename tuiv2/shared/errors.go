package shared

type UserVisibleError struct {
	Op  string
	Err error
}

func (e UserVisibleError) Error() string {
	if e.Err == nil {
		return e.Op
	}
	if e.Op == "" {
		return e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}
