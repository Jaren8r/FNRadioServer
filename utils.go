package main

import (
	"crypto/rand"
	"encoding/hex"
)

func generateID() string {
	for {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			continue
		}

		return hex.EncodeToString(bytes)
	}
}
