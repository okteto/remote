package ssh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
)

var (
	// ErrEOF is the error when the terminal exits
	ErrEOF = errors.New("EOF")
)

// Server holds the ssh server configuration
type Server struct {
	Port           int
	Shell          string
	AuthorizedKeys []ssh.PublicKey
}

// loggingWriter wraps an io.Writer and logs all data written to it
type loggingWriter struct {
	writer io.Writer
	logger *log.Entry
	name   string
	buf    *bytes.Buffer
}

func newLoggingWriter(w io.Writer, logger *log.Entry, name string) *loggingWriter {
	return &loggingWriter{
		writer: w,
		logger: logger,
		name:   name,
		buf:    &bytes.Buffer{},
	}
}

func (lw *loggingWriter) Write(p []byte) (n int, err error) {
	// Write to the actual destination
	n, err = lw.writer.Write(p)

	// Log the data
	if n > 0 {
		lw.buf.Write(p[:n])
		// Log line by line for better readability
		scanner := bufio.NewScanner(lw.buf)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				lw.logger.WithField("stream", lw.name).Info(line)
			}
		}
		// Keep any remaining partial line in the buffer
		lw.buf.Reset()
		if remaining := bytes.TrimRight(p[:n], "\n"); len(remaining) > 0 && !bytes.HasSuffix(p[:n], []byte("\n")) {
			lw.buf.Write(remaining[bytes.LastIndexByte(remaining, '\n')+1:])
		}
	}

	return n, err
}

// Flush logs any remaining buffered data
func (lw *loggingWriter) Flush() {
	if lw.buf.Len() > 0 {
		lw.logger.WithField("stream", lw.name).Info(lw.buf.String())
		lw.buf.Reset()
	}
}

// loggingReader wraps an io.Reader and logs all data read from it
type loggingReader struct {
	reader io.Reader
	logger *log.Entry
	name   string
}

func newLoggingReader(r io.Reader, logger *log.Entry, name string) *loggingReader {
	return &loggingReader{
		reader: r,
		logger: logger,
		name:   name,
	}
}

func (lr *loggingReader) Read(p []byte) (n int, err error) {
	n, err = lr.reader.Read(p)

	// Log the data
	if n > 0 {
		data := string(p[:n])
		// Only log printable characters and basic control chars
		if len(strings.TrimSpace(data)) > 0 {
			lr.logger.WithField("stream", lr.name).Infof("Input: %q", strings.TrimSpace(data))
		}
	}

	return n, err
}

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

func handlePTY(logger *log.Entry, cmd *exec.Cmd, s ssh.Session, ptyReq ssh.Pty, winCh <-chan ssh.Window) error {
	if len(ptyReq.Term) > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
	}

	f, err := pty.Start(cmd)
	if err != nil {
		logger.WithError(err).Error("failed to start pty session")
		return err
	}

	go func() {
		for win := range winCh {
			setWinsize(f, win.Width, win.Height)
		}
	}()

	// Wrap the PTY file with logging to capture input
	loggedPtyIn := newLoggingWriter(f, logger, "stdin")

	go func() {
		io.Copy(loggedPtyIn, s) // stdin - captures user input
	}()

	// Wrap the session output with logging to capture output
	loggedSessionOut := newLoggingWriter(s, logger, "stdout")

	waitCh := make(chan struct{})
	go func() {
		defer close(waitCh)
		io.Copy(loggedSessionOut, f) // stdout - captures command output
		loggedSessionOut.Flush()
	}()

	if err := cmd.Wait(); err != nil {
		logger.WithError(err).Errorf("pty command failed while waiting")
		return err
	}

	select {
	case <-waitCh:
		logger.Info("stdout finished")
	case <-time.NewTicker(1 * time.Second).C:
		logger.Info("stdout didn't finish after 1s")
	}

	return nil
}

func sendErrAndExit(logger *log.Entry, s ssh.Session, err error) {
	msg := strings.TrimPrefix(err.Error(), "exec: ")
	if _, err := s.Stderr().Write([]byte(msg)); err != nil {
		logger.WithError(err).Errorf("failed to write error back to session")
	}

	if err := s.Exit(getExitStatusFromError(err)); err != nil {
		logger.WithError(err).Errorf("pty session failed to exit")
	}
}

func handleNoTTY(logger *log.Entry, cmd *exec.Cmd, s ssh.Session) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StdoutPipe")
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StderrPipe")
		return err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.WithError(err).Errorf("couldn't get StdinPipe")
		return err
	}

	if err = cmd.Start(); err != nil {
		logger.WithError(err).Errorf("couldn't start command '%s'", cmd.String())
		return err
	}

	// Wrap stdin with logging to capture user input
	loggedStdin := newLoggingWriter(stdin, logger, "stdin")

	go func() {
		defer stdin.Close()
		if _, err := io.Copy(loggedStdin, s); err != nil {
			logger.WithError(err).Errorf("failed to write session to stdin.")
		}
		loggedStdin.Flush()
	}()

	wg := &sync.WaitGroup{}

	// Wrap stdout with logging to capture command output
	loggedStdout := newLoggingWriter(s, logger, "stdout")

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(loggedStdout, stdout); err != nil {
			logger.WithError(err).Errorf("failed to write stdout to session.")
		}
		loggedStdout.Flush()
	}()

	// Wrap stderr with logging to capture error output
	loggedStderr := newLoggingWriter(s.Stderr(), logger, "stderr")

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(loggedStderr, stderr); err != nil {
			logger.WithError(err).Errorf("failed to write stderr to session.")
		}
		loggedStderr.Flush()
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		logger.WithError(err).Errorf("command failed while waiting")
		return err
	}

	return nil
}

func (srv *Server) connectionHandler(s ssh.Session) {
	sessionID := uuid.New().String()
	startTime := time.Now()

	// Extract session metadata
	remoteAddr := s.RemoteAddr().String()
	user := s.User()
	command := s.RawCommand()

	logger := log.WithFields(log.Fields{
		"session.id":      sessionID,
		"remote.address":  remoteAddr,
		"user":            user,
	})

	// Log session start
	logger.WithFields(log.Fields{
		"command": command,
	}).Info("SSH session started")

	defer func() {
		duration := time.Since(startTime)
		s.Close()
		logger.WithFields(log.Fields{
			"duration": duration.String(),
		}).Info("SSH session closed")
	}()

	logger.Infof("starting ssh session with command '%+v'", s.RawCommand())

	cmd := srv.buildCmd(s)

	if ssh.AgentRequested(s) {
		logger.Info("agent requested")
		l, err := ssh.NewAgentListener()
		if err != nil {
			logger.WithError(err).Error("failed to start agent")
			return
		}

		defer l.Close()
		go ssh.ForwardAgentConnections(l, s)
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", "SSH_AUTH_SOCK", l.Addr().String()))
	}

	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		logger.Println("handling PTY session")
		if err := handlePTY(logger, cmd, s, ptyReq, winCh); err != nil {
			sendErrAndExit(logger, s, err)
			return
		}

		s.Exit(0)
		return
	}

	logger.Println("handling non PTY session")
	if err := handleNoTTY(logger, cmd, s); err != nil {
		sendErrAndExit(logger, s, err)
		return
	}

	s.Exit(0)
}

// LoadAuthorizedKeys loads path as an array.
// It will return nil if path doesn't exist.
func LoadAuthorizedKeys(path string) ([]ssh.PublicKey, error) {
	authorizedKeysBytes, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	authorizedKeys := []ssh.PublicKey{}
	for len(authorizedKeysBytes) > 0 {
		pubKey, _, _, rest, err := ssh.ParseAuthorizedKey(authorizedKeysBytes)
		if err != nil {
			return nil, err
		}

		authorizedKeys = append(authorizedKeys, pubKey)
		authorizedKeysBytes = rest
	}

	if len(authorizedKeys) == 0 {
		return nil, fmt.Errorf("%s was empty", path)
	}

	return authorizedKeys, nil
}

func (srv *Server) authorize(ctx ssh.Context, key ssh.PublicKey) bool {
	for _, k := range srv.AuthorizedKeys {
		if ssh.KeysEqual(key, k) {
			return true
		}
	}

	log.Println("access denied")
	return false
}

// ListenAndServe starts the SSH server using port
func (srv *Server) ListenAndServe() error {
	server := srv.getServer()
	return server.ListenAndServe()
}

func (srv *Server) getServer() *ssh.Server {
	forwardHandler := &ssh.ForwardedTCPHandler{}

	server := &ssh.Server{
		Addr:    fmt.Sprintf(":%d", srv.Port),
		Handler: srv.connectionHandler,
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

	server.SetOption(ssh.HostKeyPEM([]byte(hostKeyBytes)))

	if srv.AuthorizedKeys != nil {
		server.PublicKeyHandler = srv.authorize
	}

	return server
}

func sftpHandler(sess ssh.Session) {
	sessionID := uuid.New().String()
	startTime := time.Now()

	logger := log.WithFields(log.Fields{
		"session.id":      sessionID,
		"remote.address":  sess.RemoteAddr().String(),
		"user":            sess.User(),
		"subsystem":       "sftp",
	})

	logger.Info("SFTP session started")

	defer func() {
		duration := time.Since(startTime)
		logger.WithFields(log.Fields{
			"duration": duration.String(),
		}).Info("SFTP session closed")
	}()

	debugStream := ioutil.Discard
	serverOptions := []sftp.ServerOption{
		sftp.WithDebug(debugStream),
	}
	server, err := sftp.NewServer(
		sess,
		serverOptions...,
	)
	if err != nil {
		logger.WithError(err).Error("sftp server init error")
		return
	}
	if err := server.Serve(); err == io.EOF {
		server.Close()
		logger.Info("sftp client exited session")
	} else if err != nil {
		logger.WithError(err).Error("sftp server completed with error")
	}
}

func (srv Server) buildCmd(s ssh.Session) *exec.Cmd {
	var cmd *exec.Cmd

	if len(s.RawCommand()) == 0 {
		cmd = exec.Command(srv.Shell)
	} else {
		args := []string{"-c", s.RawCommand()}
		cmd = exec.Command(srv.Shell, args...)
	}

	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, s.Environ()...)

	fmt.Println(cmd.String())
	return cmd
}
