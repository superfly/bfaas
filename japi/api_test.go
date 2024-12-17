package japi

import (
	"net/http"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestNilErr(t *testing.T) {
	var err error
	assert.False(t, ErrorIsStatus(err, http.StatusPreconditionFailed))
}
