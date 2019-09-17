package main

import (
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"

	remoteOS "github.com/okteto/remote/pkg/os"
	"github.com/okteto/remote/pkg/ssh"
)

func main() {
	if err := remoteOS.AssertBash(); err != nil {
		log.Fatalf("failed to detect bash: %s", err)
	}

	port := 22000
	if p, ok := os.LookupEnv("PORT"); ok {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			panic(err)
		}
	}

	log.Println("ssh server started")
	log.Fatal(ssh.ListenAndServe(port))
}
