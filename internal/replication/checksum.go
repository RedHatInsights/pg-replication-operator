package replication

import (
	"crypto/sha256"
	"encoding/hex"
)

func Checksum(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
