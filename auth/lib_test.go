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
	pub, priv, err := GenKeypair()
	assert.NoError(t, err)

	now := time.Now()

	signer, err := NewSigner(priv)
	assert.NoError(t, err)

	verifier1234, err := NewVerifier(pub, "m1234", 5*time.Second)
	assert.NoError(t, err)

	verifier4321, err := NewVerifier(pub, "m4321", 5*time.Second)
	assert.NoError(t, err)

	// sign/verify works for same machine, same time.
	auth := signer(now, "m1234")
	log.Printf("auth is %s", auth)
	err = verifier1234(now, auth)
	assert.NoError(t, err)

	// verify succeeds within the liveness window
	err = verifier1234(now.Add(4*time.Second), auth)
	assert.NoError(t, err)

	// verify succeeds with small clock skew
	err = verifier1234(now.Add(-1*time.Second), auth)
	assert.NoError(t, err)

	// verify fails if you mutate the data
	bs, _ := hex.DecodeString(auth)
	altered := strings.ReplaceAll(string(bs), "m1234", "m4321")
	badSig := hex.EncodeToString([]byte(altered))
	err = verifier1234(now, badSig)
	assert.Error(t, err)
	err = verifier4321(now, badSig)
	assert.Error(t, err)

	// verify fails after liveness expires
	err = verifier1234(now.Add(6*time.Second), auth)
	assert.Error(t, err)

	// verify fails with large clock skew.
	err = verifier1234(now.Add(-3*time.Second), auth)
	assert.Error(t, err)

	// verify fails if the machine id does not match
	err = verifier4321(now, auth)
	assert.Error(t, err)
}
