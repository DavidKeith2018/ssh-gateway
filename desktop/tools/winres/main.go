package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"ssh-gateway/updater"

	"github.com/tc-hib/winres"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
func run() error {
	version := os.Getenv("GATEWAY_VERSION")
	if version == "" {
		version = updater.Version
	}
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		return fmt.Errorf("版本号格式无效")
	}

	file, err := os.Open("../packaging/icon.ico")
	if err != nil {
		return err
	}
	defer file.Close()
	icon, err := winres.LoadICO(file)
	if err != nil {
		return err
	}
	resources := winres.ResourceSet{}
	if err := resources.SetIcon(winres.RT_ICON, icon); err != nil {
		return err
	}
	manifest, err := winres.AppManifestFromXML([]byte(strings.ReplaceAll(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
<assemblyIdentity version="0.1.0.0" processorArchitecture="amd64" name="SSH.Gateway.Desktop" type="win32"/>
<trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo>
<application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings><dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware><dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness></windowsSettings></application>
</assembly>`, "0.1.0.0", version+".0")))
	if err != nil {
		return err
	}
	resources.SetManifest(manifest)
	output, err := os.Create("resource_windows_amd64.syso")
	if err != nil {
		return err
	}
	defer output.Close()
	return resources.WriteObject(output, winres.ArchAMD64)
}
