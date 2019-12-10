package main

import (
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"

	remoteOS "github.com/okteto/remote/pkg/os"
	"github.com/okteto/remote/pkg/ssh"
)

// CommitString is the commit used to build the server
var CommitString string

func main() {
	if err := remoteOS.AssertBash(); err != nil {
		log.Fatalf("failed to detect bash: %s", err)
	}

	port := 22000
	if p, ok := os.LookupEnv("OKTETO_REMOTE_PORT"); ok {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			panic(err)
		}
	}

	log.Infof("ssh server %s started in 0.0.0.0:%d\n", CommitString, port)
	log.Fatal(ssh.ListenAndServe(port))
}
