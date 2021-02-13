package ssh

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
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

func Test_connectionHandler(t *testing.T) {

	var tests = []struct {
		name      string
		command   string
		stdout    string
		stderr    string
		expectErr bool
	}{
		{
			name:    "basic",
			command: "echo hi",
			stdout:  "hi",
			stderr:  "",
		},
		{
			name:    "with-shell",
			command: `sh -c "echo hi"`,
			stdout:  "hi",
			stderr:  "",
		},
		{
			name:    "several-commands",
			command: `m=hello; echo $m`,
			stdout:  "hello",
			stderr:  "",
		},
		{
			name:    "bad-command",
			command: "badcommand",
			stdout:  "",
			//stderr:    `"badcommand": executable file not found in $PATH`,
			expectErr: true,
		},
		{
			name:    "bad-command-with-shell",
			command: `sh -c "badcommand"`,
			stdout:  "",
			// we don't check if it because the output is different between OSes
			//stderr: `sh: badcommand: command not found`
			expectErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			s.Shell = "sh"
			srv := s.getServer()

			session, _, cleanup := newTestSession(t, srv, nil)
			defer cleanup()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			session.Stderr = &stderr
			session.Stdout = &stdout

			if err := session.Run(tt.command); err != nil {
				if !tt.expectErr {
					t.Fatal(err)
				}
			}

			out := strings.TrimSuffix(stdout.String(), "\n")
			if out != tt.stdout {
				t.Errorf("bad stdout. got:\n%s\nexpected:\n%s", out, tt.stdout)
			}

			if tt.stderr != "" {
				err := strings.TrimSuffix(stderr.String(), "\n")
				if err != tt.stderr {
					t.Errorf("bad stderr. got:\n'%s'\nexpected\n'%s'", err, tt.stderr)
				}
			}
		})
	}
}

func serveOnce(srv *ssh.Server, l net.Listener) error {
	conn, e := l.Accept()
	if e != nil {
		return e
	}
	srv.ChannelHandlers = map[string]ssh.ChannelHandler{
		"session":      ssh.DefaultSessionHandler,
		"direct-tcpip": ssh.DirectTCPIPHandler,
	}
	srv.HandleConn(conn)
	return nil
}

func newLocalListener() net.Listener {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if l, err = net.Listen("tcp6", "[::1]:0"); err != nil {
			panic(fmt.Sprintf("failed to listen on a port: %v", err))
		}
	}
	return l
}

func newTestSession(t *testing.T, srv *ssh.Server, cfg *gossh.ClientConfig) (*gossh.Session, *gossh.Client, func()) {
	l := newLocalListener()
	go serveOnce(srv, l)
	return newClientSession(t, l.Addr().String(), cfg)
}

func newClientSession(t *testing.T, addr string, config *gossh.ClientConfig) (*gossh.Session, *gossh.Client, func()) {
	if config == nil {
		config = &gossh.ClientConfig{}
	}

	if config.HostKeyCallback == nil {
		config.HostKeyCallback = gossh.InsecureIgnoreHostKey()
	}

	client, err := gossh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	return session, client, func() {
		session.Close()
		client.Close()
	}
}
