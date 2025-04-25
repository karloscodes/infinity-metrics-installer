package admin

import (
	"fmt"
	"reflect"
	"testing"

	"infinity-metrics-installer/internal/logging"
)

type fakeExecutor struct {
	cmds      [][]string
	failAfter int // fail after N commands; 0 means no fail unless failAfter==1 etc.
}

func (f *fakeExecutor) ExecuteCommand(args ...string) error {
	copyArgs := make([]string, len(args))
	copy(copyArgs, args)
	f.cmds = append(f.cmds, copyArgs)
	if f.failAfter != 0 && len(f.cmds) >= f.failAfter {
		return fmt.Errorf("executor failure")
	}
	return nil
}

// makeFakeManager returns a Manager wired with a fake executor for testing.
func makeFakeManager() (*Manager, *fakeExecutor) {
	logger := logging.NewLogger(logging.Config{Level: "debug"})
	fe := &fakeExecutor{}
	mgr := newManagerWithExecutor(logger, fe)
	return mgr, fe
}

func TestCreateAdminUser(t *testing.T) {
	mgr, fe := makeFakeManager()
	email := "test@example.com"
	pass := "password123"
	if err := mgr.CreateAdminUser(email, pass); err != nil {
		t.Fatalf("CreateAdminUser returned error: %v", err)
	}
	want := [][]string{{"/app/imctl", "create-admin-user", email, pass}}
	if !reflect.DeepEqual(fe.cmds, want) {
		t.Errorf("commands mismatch\nwant %#v\ngot  %#v", want, fe.cmds)
	}
}

func TestChangeAdminPassword(t *testing.T) {
	mgr, fe := makeFakeManager()
	email := "test@example.com"
	pass := "newpass123"
	if err := mgr.ChangeAdminPassword(email, pass); err != nil {
		t.Fatalf("ChangeAdminPassword returned error: %v", err)
	}
	want := [][]string{{"/app/imctl", "change-admin-password", email, pass}}
	if !reflect.DeepEqual(fe.cmds, want) {
		t.Errorf("commands mismatch\nwant %#v\ngot  %#v", want, fe.cmds)
	}
}

func TestCreateAdminUser_Error(t *testing.T) {
	mgr, fe := makeFakeManager()
	fe.failAfter = 1
	if err := mgr.CreateAdminUser("x@y.com", "passw0rd"); err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestChangeAdminPassword_Error(t *testing.T) {
	mgr, fe := makeFakeManager()
	fe.failAfter = 1
	if err := mgr.ChangeAdminPassword("x@y.com", "pass123"); err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestSequenceCommands(t *testing.T) {
	mgr, fe := makeFakeManager()
	if err := mgr.CreateAdminUser("a@b.com", "pass1234"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ChangeAdminPassword("a@b.com", "pass4321"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/app/imctl", "create-admin-user", "a@b.com", "pass1234"},
		{"/app/imctl", "change-admin-password", "a@b.com", "pass4321"},
	}
	if !reflect.DeepEqual(fe.cmds, want) {
		t.Errorf("sequence commands mismatch\nwant %#v\ngot  %#v", want, fe.cmds)
	}
}

func TestChangeAdminPassword_FailsExecutor(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error"})
	fe := &fakeExecutor{failAfter: 1}
	mgr := newManagerWithExecutor(logger, fe)
	// Expect failure on first call
	err := mgr.ChangeAdminPassword("x@y.com", "pass")
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if len(fe.cmds) != 1 {
		t.Fatalf("expected 1 command recorded, got %d", len(fe.cmds))
	}
}
