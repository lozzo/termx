package app

import "time"

const (
	testAsyncDrainTimeout   = 200 * time.Millisecond
	testEventPollInterval   = 100 * time.Millisecond
	testShutdownWaitTimeout = 5 * time.Second
)
