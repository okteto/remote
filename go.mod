module github.com/okteto/remote

go 1.13

require (
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/gliderlabs/ssh v0.2.2
	github.com/google/uuid v1.1.1
	github.com/pkg/sftp v1.10.1
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/crypto v0.0.0-20191128160524-b544559bb6d1 // indirect
	golang.org/x/sys v0.0.0-20191128015809-6d18c012aee9 // indirect
)

replace github.com/gliderlabs/ssh v0.2.2 => github.com/rberrelleza/ssh v0.2.3-0.20191129151128-337be1657602
