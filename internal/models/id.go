package models

import (
	"crypto/rand"
	"encoding/gob"
	"fmt"

	"github.com/google/uuid"
)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

func NewID() string { return uuid.New().String() }

func NewUUIDV4() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
