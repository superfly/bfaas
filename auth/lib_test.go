package auth

import (
	"encoding/hex"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

func TestAuth(t *testing.T) {
	pub, priv, err := genKeypair()
	assert.NoError(t, err)

	now := time.Now()

	signer, err := NewSigner(priv)
	assert.NoError(t, err)

	verifier, err := NewVerifier(pub, 5*time.Second)
	assert.NoError(t, err)

	// sign/verify works for same machine, same time.
	auth := signer(now, "m1234")
	log.Printf("auth is %s", auth)
	err = verifier(now, "m1234", auth)
	assert.NoError(t, err)

	// verify succeeds within the liveness window
	err = verifier(now.Add(4*time.Second), "m1234", auth)
	assert.NoError(t, err)

	// verify fails if you mutate the data
	bs, _ := hex.DecodeString(auth)
	altered := strings.ReplaceAll(string(bs), "m1234", "m4321")
	badSig := hex.EncodeToString([]byte(altered))
	err = verifier(now, "m1234", badSig)
	assert.Error(t, err)
	err = verifier(now, "m4321", badSig)
	assert.Error(t, err)

	// verify fails after liveness expires
	err = verifier(now.Add(6*time.Second), "m1234", auth)
	assert.Error(t, err)

	// verify fails if the machine id does not match
	err = verifier(now, "m4321", auth)
	assert.Error(t, err)
}
