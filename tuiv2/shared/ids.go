package shared

import (
	"fmt"
	"sync/atomic"
	"time"
)

type IDSource interface {
	Next(prefix string) string
}

type SequentialIDSource struct {
	counter atomic.Int64
}

var shortIDCounter atomic.Uint64

func NewSequentialIDSource() *SequentialIDSource {
	return &SequentialIDSource{}
}

func (s *SequentialIDSource) Next(prefix string) string {
	n := s.counter.Add(1)
	return fmt.Sprintf("%s%d", prefix, n)
}

func GenerateShortID() string {
	n := shortIDCounter.Add(1)
	return fmt.Sprintf("%x%x", time.Now().UnixNano(), n)
}
