package requirements

import (
	"fmt"
	"net"
	"os"

	"infinity-metrics-installer/internal/logging"
)

type Checker struct {
	logger *logging.Logger
}

func NewChecker(logger *logging.Logger) *Checker {
	return &Checker{
		logger: logger,
	}
}

// CheckSystemRequirements performs all system requirement checks
func (c *Checker) CheckSystemRequirements() error {
	fmt.Println("🔍 Performing system checks...")
	fmt.Println()

	// Root privilege check
	if err := c.checkRootPrivileges(); err != nil {
		return err
	}

	// Port availability check
	if err := c.checkPortAvailability(); err != nil {
		return err
	}

	fmt.Println()
	return nil
}

// checkRootPrivileges verifies that the installer is running with root privileges
func (c *Checker) checkRootPrivileges() error {
	if os.Geteuid() != 0 && os.Getenv("ENV") != "test" {
		fmt.Printf("❌ Error: This installer must be run as root. Please run with 'sudo'.\n")
		fmt.Printf("Example: sudo %s install\n", os.Args[0])
		return fmt.Errorf("root privileges required")
	}
	fmt.Println("✅ Root privileges confirmed")
	return nil
}

// checkPortAvailability verifies that required ports are available
func (c *Checker) checkPortAvailability() error {
	// Skip port checking in integration tests
	if os.Getenv("SKIP_PORT_CHECKING") == "1" {
		fmt.Println("⚠️  Skipping port availability check (test mode)")
		return nil
	}

	fmt.Print("🔍 Checking port availability... ")

	if !c.checkPort(80) {
		fmt.Printf("\n❌ Error: Port 80 is not available - required for HTTP access and SSL certificate generation\n")
		return fmt.Errorf("port 80 is not available")
	}

	if !c.checkPort(443) {
		fmt.Printf("\n❌ Error: Port 443 is not available - required for HTTPS access and SSL certificate generation\n")
		return fmt.Errorf("port 443 is not available")
	}

	fmt.Println("✅ Ports 80 and 443 are available")
	return nil
}

// checkPort checks if a specific port is available
func (c *Checker) checkPort(port int) bool {
	address := fmt.Sprintf("localhost:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
