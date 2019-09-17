package os

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

type distribution string

var (
	debian  distribution = "debian"
	alpine  distribution = "alpine"
	unknown distribution = "unknown"
	centos  distribution = "centos"

	// ErrNoBash is used when bash is not available in the $PATH
	ErrNoBash = fmt.Errorf("bash needs to be available in the $PATH of your development environment")
)

const osRelease = "/etc/os-release"

func getDistribution() (distribution, error) {
	if fileExists(osRelease) {
		return fromOSRelease()
	}

	return unknown, nil
}

// AssertBash installs bash locally if not in the path
func AssertBash() error {
	if p, err := exec.LookPath("bash"); err == nil {
		log.Printf("bash exists at %s", p)
		return nil
	}

	d, err := getDistribution()
	if err != nil {
		log.Errorf("failed to detect the local distribution: %s", err)
		return ErrNoBash
	}

	if d == unknown {
		log.Errorf("unknown local distribution")
		return ErrNoBash
	}

	switch d {
	case debian:
		return install(exec.Command("apt-get", "update"), exec.Command("apt-get", "install", "-y"))
	case alpine:
		return install(exec.Command("apk", "update", "--no-cache"), exec.Command("apk", "add", "bash"))
	case centos:
		return install(exec.Command("yum", "-y", "update"), exec.Command("yum", "install", "-y", "bash"))
	default:
		return ErrNoBash
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func fromOSRelease() (distribution, error) {
	c, err := ioutil.ReadFile(osRelease)
	if err != nil {
		return "", err
	}

	for _, l := range strings.Split(string(c), "\n") {
		if strings.HasPrefix(l, "ID=") {
			sp := strings.Split(l, "ID=")
			switch sp[1] {
			case "ubuntu":
			case "debian":
				return debian, nil
			case "centos":
				return centos, nil
			case "alpine":
				return alpine, nil
			default:
				return unknown, nil
			}
		}
	}

	return unknown, nil
}

func install(updateCmd, installCmd *exec.Cmd) error {
	stdout, err := updateCmd.CombinedOutput()
	if err != nil {
		log.Printf(string(stdout))
		return ErrNoBash
	}

	stdout, err = installCmd.CombinedOutput()
	if err == nil {
		log.Printf("bash installed successfully")
		return nil
	}

	log.Printf(string(stdout))
	return ErrNoBash
}
