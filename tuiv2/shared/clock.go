package shared

import "time"

type Clock interface {
	Now() time.Time
}
