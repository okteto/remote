package ssh

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sync"
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

const bash = "bash"

func getExitStatusFromError(err error) int {
	if err == nil {
		return 0
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return 1
	}

	waitStatus, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		if exitErr.Success() {
			return 0
		}

		return 1
	}

	return waitStatus.ExitStatus()
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

func handlePTY(logger *log.Entry, cmd *exec.Cmd, s ssh.Session, ptyReq ssh.Pty, winCh <-chan ssh.Window) {
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

func handleNoTTY(logger *log.Entry, cmd *exec.Cmd, s ssh.Session) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StdoutPipe")
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StderrPipe")
		return
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StdinPipe")
		return
	}

	if err = cmd.Start(); err != nil {
		logger.WithError(err).Errorf("couldn't start command '%s'", cmd.String())
		return
	}

	go func() {
		defer stdin.Close()
		if _, err := io.Copy(stdin, s); err != nil {
			logger.WithError(err).Errorf("failed to write session to stdin.")
		}
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(s, stdout); err != nil {
			logger.WithError(err).Errorf("failed to write stdout to session.")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(s.Stderr(), stderr); err != nil {
			logger.WithError(err).Errorf("failed to write stderr to session.")
		}
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		logger.WithError(err).Errorf("command failed while waiting")
	}

	if err := s.Exit(getExitStatusFromError(err)); err != nil {
		logger.WithError(err).Errorf("session failed to exit")
	}
}

func connectionHandler(s ssh.Session) {
	sessionID := uuid.New().String()
	logger := log.WithFields(log.Fields{"session.id": sessionID})
	defer func() {
		s.Close()
		logger.Print("session closed")
	}()

	logger.Infof("starting ssh session with command '%+v'", s.RawCommand())

	cmd := buildCmd(s)

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

	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		logger.Println("handling PTY session")
		handlePTY(logger, cmd, s, ptyReq, winCh)
		return
	}

	handleNoTTY(logger, cmd, s)
}

// ListenAndServe starts the SSH server using port
func ListenAndServe(port int) error {
	forwardHandler := &ssh.ForwardedTCPHandler{}

	server := &ssh.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: connectionHandler,
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
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": sftpHandler,
		},
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

func buildCmd(s ssh.Session) *exec.Cmd {
	var cmd *exec.Cmd

	if len(s.RawCommand()) == 0 {
		cmd = exec.Command(bash)
	} else {
		args := []string{"-c", s.RawCommand()}
		cmd = exec.Command(bash, args...)
	}

	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, s.Environ()...)

	return cmd
}
