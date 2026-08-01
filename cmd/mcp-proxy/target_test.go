package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func captureRunTarget(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(append([]string{"target"}, args...))
	err := cmd.Execute()
	return buf.String(), errBuf.String(), err
}

func TestTargetAdd_RequiresConfirmation(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, err := captureRunTarget(t, root, "add", "--host", "x.com")
	if err == nil {
		t.Fatal("expected error without --confirm")
	}
}

func TestTargetAdd_WithConfirm(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRun(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRun(t, root, "use", "P")
	out, _, err := captureRunTarget(t, root, "add", "--host", "x.com", "--confirm")
	if err != nil {
		t.Fatalf("err: %v out=%s", err, out)
	}
	if !strings.Contains(out, "x.com") {
		t.Errorf("output missing host: %s", out)
	}
}

func TestTargetList_Empty(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRun(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRun(t, root, "use", "P")
	out, _, err := captureRunTarget(t, root, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "nenhum") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestTargetUse(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRun(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRun(t, root, "use", "P")
	_, _, _ = captureRunTarget(t, root, "add", "--host", "x.com", "--confirm")
	_, _, err := captureRunTarget(t, root, "use", "x.com")
	if err != nil {
		t.Fatal(err)
	}
}
