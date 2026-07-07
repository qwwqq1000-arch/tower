package provision

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

type sshExec struct{ client *ssh.Client }

// Dial opens a password SSH session to host:22.
//
// WARNING: host-key checking is DISABLED (InsecureIgnoreHostKey). This means
// the connection is vulnerable to man-in-the-middle attacks. Use DialWithHostKey
// and supply the pinned base64-encoded public key whenever the server's host key
// is known in advance (e.g. from the cloud provider's API or a first-connect
// fingerprint stored before provisioning). Internal provisioning of freshly-rented
// machines where the host key is not yet known may legitimately require this
// insecure path; callers should document that risk in their operational runbooks.
func Dial(host, user, password string) (Executor, func() error, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // provision-4: MITM risk documented above
		Timeout:         20 * time.Second,
	}
	return dial(host, cfg)
}

// DialWithHostKey opens a password SSH session and verifies the server's host
// key against pinnedKeyB64, a base64-encoded SSH wire-format public key (i.e.
// the output of ssh.PublicKey.Marshal() encoded with base64.StdEncoding).
//
// Use this function whenever the expected host key is known in advance. The
// connection is rejected if the server presents any other key, eliminating the
// MITM risk present in Dial.
func DialWithHostKey(host, user, password, pinnedKeyB64 string) (Executor, func() error, error) {
	pinnedBytes, err := base64.StdEncoding.DecodeString(pinnedKeyB64)
	if err != nil {
		return nil, nil, fmt.Errorf("provision ssh: invalid pinned key base64: %w", err)
	}
	pinnedPub, err := ssh.ParsePublicKey(pinnedBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("provision ssh: parse pinned public key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.FixedHostKey(pinnedPub),
		Timeout:         20 * time.Second,
	}
	return dial(host, cfg)
}

// dial is the shared connection helper.
func dial(host string, cfg *ssh.ClientConfig) (Executor, func() error, error) {
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
