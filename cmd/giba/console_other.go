//go:build !windows

package main

func hideConsole() {
	// Windows以外では何もしない
}

func showConsole() {
	// Windows以外では何もしない
}
