package ssh

import (
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func Test_loadPrivateKey(t *testing.T) {
	_, err := gossh.ParsePrivateKey([]byte(privateKeyBytes))
	if err != nil {
		t.Error(err)
	}
}
