package main

import (
	"fmt"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"

	remoteOS "github.com/okteto/remote/pkg/os"
	"github.com/okteto/remote/pkg/ssh"
)

// CommitString is the commit used to build the server
var CommitString string

const (
	authorizedKeysPath = "/var/okteto/remote/authorized_keys"
	shell              = "bash"
)

func main() {
	log.SetOutput(os.Stdout)
	if err := remoteOS.AssertBash(); err != nil {
		log.Fatalf("failed to detect bash: %s", err)
	}

	port := 2222
	if p, ok := os.LookupEnv("OKTETO_REMOTE_PORT"); ok {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			panic(fmt.Sprintf("%s is not a valid port number", p))
		}

		if port <= 1024 {
			panic(fmt.Sprintf("%d is a reserved port", port))
		}
	}

	keys, err := ssh.LoadAuthorizedKeys(authorizedKeysPath)
	if err != nil {
		log.Fatalf("Failed to load authorized_keys: %s", err)
	}

	if keys == nil {
		log.Warningf("remote server is running without authentication enabled")
	}

	srv := ssh.Server{
		Port:           port,
		Shell:          shell,
		AuthorizedKeys: keys,
	}

	log.Infof("ssh server %s started in 0.0.0.0:%d", CommitString, srv.Port)
	log.Fatal(srv.ListenAndServe())
}
