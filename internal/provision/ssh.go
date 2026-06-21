package provision

import (
	"bytes"
	"context"
	"time"

	"golang.org/x/crypto/ssh"
)

type sshExec struct{ client *ssh.Client }

// Dial opens a password SSH session to host:22. Host-key checking is disabled
// (internal provisioning of freshly-rented machines).
func Dial(host, user, password string) (Executor, func() error, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	addr := host
	if !hasPort(host) {
		addr = host + ":22"
	}
	c, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, err
	}
	return &sshExec{client: c}, c.Close, nil
}

func hasPort(h string) bool {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i] == ':' {
			return true
		}
		if h[i] == ']' {
			return false
		}
	}
	return false
}

func (e *sshExec) Exec(ctx context.Context, cmd string) (ExecResult, error) {
	sess, err := e.client.NewSession()
	if err != nil {
		return ExecResult{}, err
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errb
	runErr := sess.Run(cmd)
	code := 0
	if ee, ok := runErr.(*ssh.ExitError); ok {
		code = ee.ExitStatus()
		runErr = nil
	}
	return ExecResult{Stdout: out.String(), Stderr: errb.String(), Code: code}, runErr
}
