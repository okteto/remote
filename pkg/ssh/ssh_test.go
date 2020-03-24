package ssh

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

const (
	goodKey = `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDBIpLXzFcYKRBoB76vrUWaD0SZ9tstmZfoPX1ZF0rrK7ZLk9kVD+vGDdmPALSAvUCk/WM1h4BNa57SY6KjmVrbcVVYSW/4i7Vnp/KIsx9D5Tkj+Ytu2VFHpLm7ocnCqEoB1iP8edatbogkIh7fJ5HszfD3d47PU6dA8tMonIlLCjfwQO1FFkJ5V353L+5JLQpGlsDidYjUHXvC6j7zJlvEgtxImuDyRNvpJJ6QZhDJz2GeRuaR+ZFbjVFL7Q4AqDlYbNDH3Whi/Uv3ZByrBQcARXbcvqWI/DbKQJCoaq8Xl+G3EAwSClaF2U2DTWWs8VDmiHNbXYaNJqppGObfHkh9 test@example.com`
	badKey  = `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDR2feI3OWc8HUdCQyWDEF3+AwClRktiXPQebSByfN23IMoJLVpyf/zWjBtdBFjXrUPqlcPwCwMw85qixtNa1cjUk/PikYIFsvX4xkkRq4ufdsYu/DF7bcIb704qEITIXanToc0bJX0Sx/3OmP1d0X9GxKP++gFAdUNSXDGcTp5bAnfDLYQM+HgakI/v/h25zfz4f0XkFXcU7NHp7mE29ssyka7JilWZa9/Aah24mOZ8j0U2D9yS67hTd84tJ5mUrruR7WsXfFGb4pCwos3VVW5xhBm8aymSka6j24mQK9jH6ZcbKbrElgeTNNA1YHJTYISrj1V0ors4ivS2J+Y5bzV test@example.com`
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

	parsed, _, _, _, err := gossh.ParseAuthorizedKey([]byte(goodKey))
	if err != nil {
		t.Fatalf("failed to parse key: %s", err)
	}

	if _, err := path.WriteString(goodKey); err != nil {
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

	srv := Server{AuthorizedKeys: k}
	if !srv.authorize(nil, parsed) {
		t.Error("failed to authorize loaded key")
	}

	bad, _, _, _, err := gossh.ParseAuthorizedKey([]byte(badKey))
	if err != nil {
		t.Fatalf("failed to parse key: %s", err)
	}

	if srv.authorize(nil, bad) {
		t.Error("authorized bad key")
	}
}

func TestLoadAuthorizedKeys_multiple(t *testing.T) {
	// empty file
	path, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(path.Name())

	for i := 0; i < 3; i++ {
		if _, err := path.WriteString(goodKey + "\n"); err != nil {
			t.Fatal(err)
		}
	}

	parsed, _, _, _, err := gossh.ParseAuthorizedKey([]byte(goodKey))
	if err != nil {
		t.Fatalf("failed to parse key: %s", err)
	}

	k, err := LoadAuthorizedKeys(path.Name())
	if err != nil {
		t.Error(err)
	}

	if len(k) != 3 {
		t.Error("didn't load 3 authorized keys")
	}

	if !ssh.KeysEqual(k[0], parsed) {
		t.Error("loaded key is not the same")
	}

	srv := Server{AuthorizedKeys: k}
	if !srv.authorize(nil, k[1]) {
		t.Error("failed to authorize loaded key")
	}
}
