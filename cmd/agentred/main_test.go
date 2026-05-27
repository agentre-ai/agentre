package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRootSubcommands locks in the public CLI surface: any accidental rename
// or removal of a subcommand here breaks the test, which is much louder than
// users discovering it at runtime.
func TestRootSubcommands(t *testing.T) {
	root := newRootCmd()
	got := map[string]bool{}
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}
	for _, want := range []string{"run", "status", "pair", "llm", "claudecode"} {
		assert.True(t, got[want], "missing subcommand %q", want)
	}

	llm, _, err := root.Find([]string{"llm"})
	assert.NoError(t, err)
	llmSubs := map[string]bool{}
	for _, c := range llm.Commands() {
		llmSubs[c.Name()] = true
	}
	for _, want := range []string{"list", "add", "remove"} {
		assert.True(t, llmSubs[want], "missing llm subcommand %q", want)
	}
}

func TestLLMAddRequiresUUIDKey(t *testing.T) {
	root := newRootCmd()

	// --key not provided → cobra required-flag error (not usageError, but still an error)
	root.SetArgs([]string{"llm", "add", "--name=test", "--type=openai"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	assert.Error(t, err, "missing --key should be an error")

	// --key is not a valid UUID → usageError
	root2 := newRootCmd()
	root2.SetArgs([]string{"llm", "add", "--key=not-a-uuid", "--name=test", "--type=openai", "--api-key=x"})
	root2.SetOut(&bytes.Buffer{})
	root2.SetErr(&bytes.Buffer{})
	err2 := root2.Execute()
	assert.Error(t, err2)
	_, ok := err2.(*usageError)
	assert.True(t, ok, "invalid UUID should produce usageError, got %T: %v", err2, err2)
}

func TestLLMRemoveRequiresUUIDKey(t *testing.T) {
	root := newRootCmd()

	// old positional numeric arg is gone; --key with non-UUID → usageError
	root.SetArgs([]string{"llm", "remove", "--key=not-a-uuid"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	assert.Error(t, err)
	_, ok := err.(*usageError)
	assert.True(t, ok, "non-UUID --key should produce usageError, got %T: %v", err, err)
}

func TestUsageErrorExitCode(t *testing.T) {
	// old positional-arg numeric remove is gone; --key with non-UUID produces usageError
	root := newRootCmd()
	root.SetArgs([]string{"llm", "remove", "--key=not-a-uuid"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	assert.Error(t, err)
	_, ok := err.(*usageError)
	assert.True(t, ok, "expected usageError, got %T", err)
}

func TestRunFlagDefaults(t *testing.T) {
	root := newRootCmd()
	runCmd, _, err := root.Find([]string{"run"})
	assert.NoError(t, err)

	port, err := runCmd.Flags().GetInt("port")
	assert.NoError(t, err)
	assert.Equal(t, 7456, port)

	host, err := runCmd.Flags().GetString("host")
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", host)

	assert.Nil(t, runCmd.Flags().Lookup("key-storage"))
}

func TestRootHelpMentionsBinary(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	assert.NoError(t, root.Execute())
	assert.True(t, strings.Contains(buf.String(), "agentred"))
}
