package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewRuntimeInstanceID() string {
	return fmt.Sprintf("rtm_%s", randomHex(8))
}

func NewSessionID() string {
	return fmt.Sprintf("sess_%s_%s", time.Now().UTC().Format("20060102T150405Z"), randomHex(4))
}

func NewExecID() string {
	return fmt.Sprintf("exec_%s_%s", time.Now().UTC().Format("20060102T150405Z"), randomHex(4))
}

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("generate random id: %v", err))
	}
	return hex.EncodeToString(buf)
}
