package hookexec

import (
	"errors"
	"testing"
)

func TestResolve_UnknownInterpreter(t *testing.T) {
	if _, err := Resolve("ruby"); !errors.Is(err, ErrUnknownInterpreter) {
		t.Fatalf("expected ErrUnknownInterpreter, got %v", err)
	}
}

func TestResolve_KnownInterpreterShape(t *testing.T) {
	// sh 在类 Unix CI 一定在；不在则跳过（Windows）。
	in, err := Resolve("sh")
	if errors.Is(err, ErrInterpreterNotInstalled) {
		t.Skip("sh not installed on this platform")
	}
	if err != nil {
		t.Fatalf("Resolve sh: %v", err)
	}
	if in.Bin == "" || in.Ext != ".sh" {
		t.Fatalf("unexpected interp: %+v", in)
	}
}

func TestResolve_PwshArgs(t *testing.T) {
	in, err := Resolve("pwsh")
	if errors.Is(err, ErrInterpreterNotInstalled) {
		t.Skip("pwsh not installed")
	}
	if err != nil {
		t.Fatalf("Resolve pwsh: %v", err)
	}
	if len(in.Args) == 0 || in.Args[len(in.Args)-1] != "-File" {
		t.Fatalf("pwsh should pass -File before script path: %+v", in.Args)
	}
}
