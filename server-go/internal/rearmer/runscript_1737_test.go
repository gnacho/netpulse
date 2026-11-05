package rearmer

import (
	"testing"
	"time"
)

// scriptPool implementa Run y RunScript: el rearmer tiene que elegir el
// segundo para el script de reinstalación, que pasa del límite de argv de
// dropbear (#1737).
type scriptPool struct {
	cmd    string
	script string
}

func (p *scriptPool) Run(_ string, cmd string, _ time.Duration) (string, error) {
	p.cmd = cmd
	return "", nil
}

func (p *scriptPool) RunScript(_ string, script string, _ time.Duration) (string, error) {
	p.script = script
	return "", nil
}

// argvOnlyPool es lo que implementan los fakes de test: solo Run.
type argvOnlyPool struct {
	cmd string
}

func (p *argvOnlyPool) Run(_ string, cmd string, _ time.Duration) (string, error) {
	p.cmd = cmd
	return "", nil
}

func TestRunScriptSendsByStdinWhenThePoolCan(t *testing.T) {
	p := &scriptPool{}
	if _, err := runScript(p, "192.168.1.2", "echo hola", time.Second); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if p.script != "echo hola" {
		t.Fatalf("script by stdin = %q, want %q", p.script, "echo hola")
	}
	if p.cmd != "" {
		t.Fatalf("also went through argv: %q", p.cmd)
	}
}

func TestRunScriptFallsBackToArgvWithoutStdinSupport(t *testing.T) {
	p := &argvOnlyPool{}
	if _, err := runScript(p, "192.168.1.2", "echo hola", time.Second); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if p.cmd != "echo hola" {
		t.Fatalf("argv = %q, want %q", p.cmd, "echo hola")
	}
}
