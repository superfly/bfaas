package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/sign"
)

const signPrivKeySize = 64
const signPubKeySize = 32

var ErrBadAuth = fmt.Errorf("Authentication failed")
var timeSlack = 5 * time.Second

// randomBytes returns sz random bytes.
// It should never fail, but if it does, it will panic.
func randomBytes(sz int) []byte {
	buf := make([]byte, sz)
	n, err := rand.Read(buf)
	if n != sz || err != nil {
		log.Panicf("crypto random failed: %d read of %d: err: %s", n, sz, err)
	}

	return buf
}

// GenKeypair returns a newly generated public and private key.
func GenKeypair() (string, string, error) {
	pub, priv, err := sign.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}

	return hex.EncodeToString(pub[:]), hex.EncodeToString(priv[:]), nil
}

// parseKey parses a hex-encoded key that is expected to be sz bytes long.
func parseKey(s string, sz int) ([]byte, error) {
	bs, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(bs) != sz {
		return nil, fmt.Errorf("Key is malformed")
	}

	return bs, nil
}

type Signer func(now time.Time, mach string) string

func NewSigner(hexPrivKey string) (Signer, error) {
	privKeyBs, err := parseKey(hexPrivKey, signPrivKeySize)
	if err != nil {
		return nil, fmt.Errorf("Error parsing private key: %w", err)
	}
	privKey := (*[signPrivKeySize]byte)(privKeyBs)

	return func(now time.Time, machId string) string {
		msg := []byte(newMsg(now, machId))
		sig := make([]byte, 0, len(msg)+sign.Overhead)
		sig = sign.Sign(sig, msg, privKey)
		return hex.EncodeToString(sig)
	}, nil
}

// newMsg makes an auth message with ts and machId.
func newMsg(ts time.Time, machId string) string {
	return fmt.Sprintf("%d,%s", ts.Unix(), machId)
}

// parseMsg parses an auth message into ts and machId.
func parseMsg(msg string) (ts time.Time, machId string, err error) {
	ws := strings.Split(string(msg), ",")
	if len(ws) != 2 {
		err = fmt.Errorf("malformed, need two fields")
		return
	}

	unix, err := strconv.ParseInt(ws[0], 10, 64)
	if err != nil {
		err = fmt.Errorf("bad time field: %w", err)
		return
	}

	ts = time.Unix(unix, 0)
	machId = ws[1]
	return
}

type Verifier func(now time.Time, auth string) error

func NewVerifier(hexPubKey string, targMachId string, liveness time.Duration) (Verifier, error) {
	pubKeyBs, err := parseKey(hexPubKey, signPubKeySize)
	if err != nil {
		return nil, fmt.Errorf("Error parsing public key: %w", err)
	}
	pubKey := (*[signPubKeySize]byte)(pubKeyBs)

	return func(now time.Time, auth string) error {
		sig, err := hex.DecodeString(auth)
		if err != nil {
			return ErrBadAuth
		}

		msg, ok := sign.Open(nil, sig, pubKey)
		if !ok {
			log.Printf("bad signature for %s", auth)
			return ErrBadAuth
		}

		ts, machId, err := parseMsg(string(msg))
		if err != nil {
			log.Printf("bad message format: %v", err)
			return ErrBadAuth
		}

		dt := now.Sub(ts)
		if !(-timeSlack < dt && dt < liveness) {
			log.Printf("bad ts %v (dt=%v)", ts, dt)
			return ErrBadAuth
		}

		if machId != targMachId {
			log.Printf("bad machId %v != %v", machId, targMachId)
			return ErrBadAuth
		}

		return nil
	}, nil
}
