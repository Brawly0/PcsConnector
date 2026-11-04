//go:build windows

package main

// One-click virtual monitor: installs the bundled, signed Virtual Display Driver (IddCx) with one UAC prompt.

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

//go:embed vdd/*
var vddFiles embed.FS

var (
	setupapi                 = syscall.NewLazyDLL("setupapi.dll")
	newdev                   = syscall.NewLazyDLL("newdev.dll")
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	pIsUserAnAdmin           = shell32.NewProc("IsUserAnAdmin")
	pSetupDiCreateInfoList   = setupapi.NewProc("SetupDiCreateDeviceInfoList")
	pSetupDiCreateDeviceInfo = setupapi.NewProc("SetupDiCreateDeviceInfoW")
	pSetupDiSetRegProp       = setupapi.NewProc("SetupDiSetDeviceRegistryPropertyW")
	pSetupDiCallClassInst    = setupapi.NewProc("SetupDiCallClassInstaller")
	pSetupDiDestroy          = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	pUpdateDriver            = newdev.NewProc("UpdateDriverForPlugAndPlayDevicesW")
)

type guid struct {
	d1 uint32
	d2 uint16
	d3 uint16
	d4 [8]byte
}

type spDevinfoData struct {
	size     uint32
	class    guid
	devInst  uint32
	reserved uintptr
}

var guidDisplay = guid{0x4D36E968, 0xE325, 0x11CE, [8]byte{0xBF, 0xC1, 0x08, 0x00, 0x2B, 0xE1, 0x03, 0x18}}

func vmonStatus() map[string]any {
	return map[string]any{"supported": true, "installed": hasVirtualDriver()}
}

func vddLog() string { return filepath.Join(os.TempDir(), "pcsc-vdd.log") }

// installDisplay is called from the UI: elevates a copy of this program and waits for the monitor to appear.
func installDisplay() error {
	if hasVirtualDriver() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	os.Remove(vddLog())
	c := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command",
		fmt.Sprintf("Start-Process -FilePath '%s' -ArgumentList 'install-display' -Verb RunAs -Wait", exe))
	hideWindow(c)
	if err := c.Run(); err != nil {
		return fmt.Errorf("Windows permission was declined - press the button again and choose Yes")
	}
	for i := 0; i < 20; i++ {
		if hasVirtualDriver() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if b, err := os.ReadFile(vddLog()); err == nil && len(b) > 0 {
		return fmt.Errorf("driver install failed: %s", b)
	}
	return fmt.Errorf("the virtual monitor did not appear - try again, or restart Windows once")
}

// runInstallDisplay is the elevated `pcsc install-display` subcommand.
func runInstallDisplay() {
	err := doInstallDisplay()
	if err != nil {
		os.WriteFile(vddLog(), []byte(err.Error()), 0o644)
		os.Exit(1)
	}
}

func lastErr(r uintptr, e error, what string) error {
	if r == 0 {
		return fmt.Errorf("%s: %v", what, e)
	}
	return nil
}

func doInstallDisplay() error {
	if r, _, _ := pIsUserAnAdmin.Call(); r == 0 {
		return fmt.Errorf("administrator rights are required")
	}
	dir := filepath.Join(os.Getenv("ProgramData"), "PcsConnector", "vdd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	es, _ := vddFiles.ReadDir("vdd")
	for _, e := range es {
		b, _ := vddFiles.ReadFile("vdd/" + e.Name())
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	// the driver reads its settings from here
	os.MkdirAll(`C:\VirtualDisplayDriver`, 0o755)
	if _, err := os.Stat(`C:\VirtualDisplayDriver\vdd_settings.xml`); err != nil {
		b, _ := vddFiles.ReadFile("vdd/vdd_settings.xml")
		os.WriteFile(`C:\VirtualDisplayDriver\vdd_settings.xml`, b, 0o644)
	}
	inf := filepath.Join(dir, "MttVDD.inf")

	set, _, e := pSetupDiCreateInfoList.Call(uintptr(unsafe.Pointer(&guidDisplay)), 0)
	if set == ^uintptr(0) {
		return fmt.Errorf("SetupDiCreateDeviceInfoList: %v", e)
	}
	defer pSetupDiDestroy.Call(set)
	name, _ := syscall.UTF16PtrFromString("Display")
	dev := spDevinfoData{size: uint32(unsafe.Sizeof(spDevinfoData{}))}
	const dicdGenerateID = 1
	r, _, e := pSetupDiCreateDeviceInfo.Call(set, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&guidDisplay)), 0, 0, dicdGenerateID, uintptr(unsafe.Pointer(&dev)))
	if err := lastErr(r, e, "SetupDiCreateDeviceInfo"); err != nil {
		return err
	}
	hw, _ := syscall.UTF16FromString(`Root\MttVDD`)
	hw = append(hw, 0) // multi-sz double terminator
	const spdrpHardwareID = 1
	r, _, e = pSetupDiSetRegProp.Call(set, uintptr(unsafe.Pointer(&dev)), spdrpHardwareID, uintptr(unsafe.Pointer(&hw[0])), uintptr(len(hw)*2))
	if err := lastErr(r, e, "SetupDiSetDeviceRegistryProperty"); err != nil {
		return err
	}
	const difRegisterDevice = 0x19
	r, _, e = pSetupDiCallClassInst.Call(difRegisterDevice, set, uintptr(unsafe.Pointer(&dev)))
	if err := lastErr(r, e, "SetupDiCallClassInstaller"); err != nil {
		return err
	}
	infp, _ := syscall.UTF16PtrFromString(inf)
	hwp, _ := syscall.UTF16PtrFromString(`Root\MttVDD`)
	var reboot int32
	const installFlagForce = 1
	r, _, e = pUpdateDriver.Call(0, uintptr(unsafe.Pointer(hwp)), uintptr(unsafe.Pointer(infp)), installFlagForce, uintptr(unsafe.Pointer(&reboot)))
	return lastErr(r, e, "UpdateDriverForPlugAndPlayDevices")
}
