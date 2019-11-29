package ssh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"github.com/kr/pty"
	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
)

var (
	idleTimeout = 60 * time.Second

	// ErrEOF is the error when the terminal exits
	ErrEOF = errors.New("EOF")
)

type exitStatusMsg struct {
	Status uint32
}

func exitStatus(err error) (exitStatusMsg, error) {
	if err != nil {
		if ErrEOF == err {
			return exitStatusMsg{0}, nil
		}

		if exiterr, ok := err.(*exec.ExitError); ok {
			// There is no platform independent way to retrieve
			// the exit code, but the following will work on Unix
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return exitStatusMsg{uint32(status.ExitStatus())}, nil
			}
		}
		return exitStatusMsg{0}, err
	}
	return exitStatusMsg{0}, nil
}

func assert(at string, err error, s ssh.Session) bool {
	if err != nil {
		log.Printf("%s failed: %s", at, err)
		s.Write([]byte("internal error\n"))
		return true
	}

	return false
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

func handlePTY(logger *log.Entry, s ssh.Session, ptyReq ssh.Pty, winCh <-chan ssh.Window) {
	cmd := exec.Command("bash")
	cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
	cmd.Env = append(cmd.Env, os.Environ()...)

	f, err := pty.Start(cmd)
	if err != nil {
		logger.WithField("error", err).Error("failed to start pty session")
		return
	}

	go func() {
		for win := range winCh {
			setWinsize(f, win.Width, win.Height)
		}
	}()

	go func() {
		io.Copy(f, s) // stdin
	}()
	io.Copy(s, f) // stdout
	cmd.Wait()
}

func connectionHandler(s ssh.Session) {
	sessionID := uuid.New().String()
	logger := log.WithFields(log.Fields{"session.id": sessionID})
	defer func() {
		s.Close()
		logger.Print("session closed")
	}()

	logger.Printf("starting ssh session with command %+v\n", s.RawCommand())
	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		logger.Println("handling PTY session")
		handlePTY(logger, s, ptyReq, winCh)
		return
	}

	c := s.Command()
	executable := c[0]
	var args []string
	if len(c) > 1 {
		args = c[1:]
	}

	path, err := exec.LookPath(executable)
	if err == nil {
		executable = path
	}

	execPath, err := filepath.Abs(executable)
	if err != nil {
		io.WriteString(s, fmt.Sprintf("unable to locate executable: %s\n", executable))
		return
	}

	cmd := exec.Command(execPath, args...)
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, s.Environ()...)
	cmd.Stdout = s
	cmd.Stderr = s.Stderr()

	if ssh.AgentRequested(s) {
		logger.Printf("agent requested")
		l, err := ssh.NewAgentListener()
		if err != nil {
			logger.WithField("error", err).Error("failed to start agent")
			return
		}

		defer l.Close()
		go ssh.ForwardAgentConnections(l, s)
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", "SSH_AUTH_SOCK", l.Addr().String()))
	}

	stdinPipe, err := cmd.StdinPipe()
	if assert("exec cmd.StdinPipe", err, s) {
		return
	}

	go func() {
		defer stdinPipe.Close()
		io.Copy(stdinPipe, s)
	}()

	status, err := exitStatus(cmd.Run())
	if !assert("exit", err, s) {
		a := make([]byte, 4)
		binary.LittleEndian.PutUint32(a, status.Status)
		if _, err = s.SendRequest("exit-status", false, a); err != nil {
			assert("exit", err, s)
		}
	}
}

// ListenAndServe starts the SSH server using port
func ListenAndServe(port int) error {
	forwardHandler := &ssh.ForwardedTCPHandler{}

	server := &ssh.Server{
		Addr:        fmt.Sprintf(":%d", port),
		IdleTimeout: 30 * time.Second,
		Handler:     connectionHandler,
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": ssh.DirectTCPIPHandler,
			"session":      ssh.DefaultSessionHandler,
		},
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("Accepted forward", dhost, dport)
			return true
		}),
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			log.Println("attempt to bind", host, port, "granted")
			return true
		}),
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		//SubsystemHandlers: map[string]ssh.SubsystemHandler{
		//	"sftp": sftpHandler,
		//},
	}

	server.SetOption(ssh.HostKeyPEM([]byte(privateKeyBytes)))

	return server.ListenAndServe()

}

func sftpHandler(sess ssh.Session) {
	debugStream := ioutil.Discard
	serverOptions := []sftp.ServerOption{
		sftp.WithDebug(debugStream),
	}
	server, err := sftp.NewServer(
		sess,
		serverOptions...,
	)
	if err != nil {
		log.Printf("sftp server init error: %s\n", err)
		return
	}
	if err := server.Serve(); err == io.EOF {
		server.Close()
		log.Println("sftp client exited session.")
	} else if err != nil {
		log.Println("sftp server completed with error:", err)
	}
}
