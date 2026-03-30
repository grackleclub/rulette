package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

// envOr returns the value of the environment variable named by the key,
// or fallback if the variable is not present.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// randHex returns a random hexadecimal string of length 2*n.
func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
