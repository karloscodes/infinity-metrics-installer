package testrunner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// VMProvider is an interface for managing VMs for testing
type VMProvider interface {
	// Create creates a VM with the given name and arguments
	Create(name string, args []string) error
	
	// Delete deletes a VM with the given name
	Delete(name string) error
	
	// Exec executes a command in the VM and returns the output
	Exec(name string, command ...string) (string, error)
	
	// Transfer copies a file to the VM
	Transfer(source, destination string) error
	
	// IsInstalled checks if the VM provider is installed
	IsInstalled() bool
	
	// Name returns the name of the VM provider
	Name() string
}

// NewVMProvider returns the appropriate VM provider for the current platform
func NewVMProvider() VMProvider {
	// Always use Multipass
	return &MultipassProvider{}
}

// MultipassProvider implements VMProvider using Multipass
type MultipassProvider struct{}

// Name returns the name of the VM provider
func (p *MultipassProvider) Name() string {
	return "Multipass"
}

// IsInstalled checks if Multipass is installed
func (p *MultipassProvider) IsInstalled() bool {
	_, err := exec.LookPath("multipass")
	return err == nil
}

// Create creates a new VM with the given name and arguments
func (p *MultipassProvider) Create(name string, args []string) error {
	cmdArgs := append([]string{"launch", "--name", name}, args...)
	cmd := exec.Command("multipass", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Delete deletes a VM with the given name
func (p *MultipassProvider) Delete(name string) error {
	cmd := exec.Command("multipass", "delete", name, "--purge")
	return cmd.Run()
}

// Exec executes a command in the VM and returns the output
func (p *MultipassProvider) Exec(name string, command ...string) (string, error) {
	cmdArgs := append([]string{"exec", name, "--"}, command...)
	cmd := exec.Command("multipass", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %w\nStderr: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// Transfer copies a file to the VM
func (p *MultipassProvider) Transfer(source, destination string) error {
	cmd := exec.Command("multipass", "transfer", source, destination)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to transfer file: %w\nStderr: %s", err, stderr.String())
	}
	return nil
}




