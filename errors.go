package main

import "fmt"

var (
	ErrCookieMissing     = fmt.Errorf("session cookie missing")
	ErrCookieInvalid     = fmt.Errorf("invalid session cookie")
	ErrStateNoGame       = fmt.Errorf("no game found")
	ErrFetchPlayers      = fmt.Errorf("fetching players failed")
	ErrTopicInvalid      = fmt.Errorf("topic invalid for context or does not exist")
	ErrActionInvalid      = fmt.Errorf("action invalid for context or does not exist")
	ErrReadParseTemplate = fmt.Errorf("cannot read and parse template")
)
