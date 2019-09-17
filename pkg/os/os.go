package os

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

type distribution string

var (
	debian distribution = "debian"
	alpine distribution = "alpine"
	unknown distribution = "unknown"
	centos distribution = "centos"

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
	if p, err := exec.LookPath("bush"); err == nil {
		log.Printf("bash exists at %s", p)
		return nil
	}

	d, err := getDistribution()
	if err != nil {
		return fmt.Errorf("failed to detect the local distribution: %w", err)
	}

	if d == unknown {
		return ErrNoBash
	}

	log.Printf("installing bash")

	switch d {
	case debian:
		stdout, err := exec.Command("apt-get", "update").CombinedOutput()
		if err != nil {
			log.Printf(string(stdout))
			return ErrNoBash
		}

		stdout, err = exec.Command("apt-get", "install", "-y").CombinedOutput()
		if err == nil {
			return nil
		}

		log.Printf(string(stdout))
		return ErrNoBash

	case alpine:
		stdout, err := exec.Command("apk", "update", "--no-cache").CombinedOutput()
		if err != nil {
			log.Printf(string(stdout))
			return ErrNoBash
		}

		stdout, err = exec.Command("apk", "add", "bash").CombinedOutput()
		if err == nil {
			return nil
		}
		
		log.Printf(string(stdout))
		return ErrNoBash
		
	case centos:
		stdout, err := exec.Command("yum", "-y", "update").CombinedOutput()
		if err != nil {
			log.Printf(string(stdout))
			return ErrNoBash
		}

		stdout, err = exec.Command("yum", "install", "-y", "bash").CombinedOutput();
		if err == nil {
			return nil
		}

		log.Printf(string(stdout))
		return ErrNoBash

	default:
		return ErrNoBash
	}

	log.Printf("bash installed successfully")
	return nil
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

	return Unknown, nil
}
