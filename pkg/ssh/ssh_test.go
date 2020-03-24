package ssh

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func Test_loadPrivateKey(t *testing.T) {
	_, err := gossh.ParsePrivateKey([]byte(hostKeyBytes))
	if err != nil {
		t.Error(err)
	}
}

func TestLoadAuthorizedKeys(t *testing.T) {
	// missing file
	k, err := LoadAuthorizedKeys("missing")
	if err != nil {
		t.Error(err)
	}

	if k != nil {
		t.Errorf("didn't return nil array")
	}

	// empty file
	path, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(path.Name())

	if _, err := LoadAuthorizedKeys(path.Name()); err == nil {
		t.Error("empty file didn't fail")
	}

	pubkey := `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQCuALm7hOVj7inLN7VkSj7RIQlRORTwuMYDVOLoxSLbXaWV6A/+zuChMGFjn7XZDl8FWok6CygTntkPqDIvCnQYkiqohtoxCwUgS8qbWHeiYVwI0RvBk/8dGLtQRaR/bMhqaChvMMIrSvKOrxC7QfIi/F18+q132f6ZbizjZHSvAjoaeCJftc9JV7VC/VoS6ctYIeVouC2xC6+Cp8BQGuR8jVnSo4qTZsLH4mV+/OkEaCa5og2C43FzOQkm8IsTpk4CBhaWbTxIkWVBXftST6E2ijc0N+BrRRyZc78sQv5nmDkAbfIf4EqtITR/7CXklu64zznJUy0HyhmhXd8kOWaWSL80augTnYgaPT6r0lP2Xz85aInT281Twm/edgCGGkYMxNNzzYLVSH5lo/+TGQSQOlmvgdGjxVsE4x25tZybvbIPzmLwA9QzF6H/t8G83S6ZMZShx1ax1y8BkZ45b/LslEj/t0wU/wnNjG+RBeCGIA73GSX+aCJBHs+Ie4a6T++jP3dQLysMrPH3XA2+M4J2zfNRqFAUSjP7Ub3pHG5p5uUeoot2yXMy6CDHyScYlZQ91SyEYLteav8WpSNcogdp5mEzQQiZlgJjVTGpAkpnfvOjP508RYC7HqWlYEkAtMmQYXWiCGEdNpF2Vxdn1JAK0U8GnOyvlhMtXhHuqdSiUw==`

	parsed, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pubkey))
	if err != nil {
		t.Fatalf("failed to parse key: %s", err)
	}

	if _, err := path.WriteString(pubkey); err != nil {
		t.Fatal(err)
	}

	k, err = LoadAuthorizedKeys(path.Name())
	if err != nil {
		t.Error(err)
	}

	if len(k) != 1 {
		t.Error("loaded more than 1 key")
	}

	if !ssh.KeysEqual(k[0], parsed) {
		t.Error("loaded key is not the same")
	}
}
