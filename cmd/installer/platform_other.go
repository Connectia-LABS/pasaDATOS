//go:build !windows

package main

import "errors"

func platformIsElevated() bool                            { return false }
func platformElevate(args []string) error                 { return errors.New("solo disponible en Windows") }
func platformMessage(title, message string, isError bool) {}
func platformCreateShortcuts(exe, icon, workingDir string) error {
	return errors.New("solo disponible en Windows")
}
func platformRegisterInstall(exe, uninstaller, icon, installDir, version string) error {
	return errors.New("solo disponible en Windows")
}
func platformAddFirewallRule(exe string) error     { return errors.New("solo disponible en Windows") }
func platformRemoveFirewallRule() error            { return nil }
func platformLaunch(exe string) error              { return errors.New("solo disponible en Windows") }
func platformStopApp() error                       { return nil }
func platformRemoveRegistration() error            { return nil }
func platformRemoveShortcuts() error               { return nil }
func platformScheduleRemoval(paths []string) error { return errors.New("solo disponible en Windows") }
