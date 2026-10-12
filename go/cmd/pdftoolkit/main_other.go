//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "PDF Toolkit GUI предназначена для запуска на Windows (GOOS=windows).")
	os.Exit(1)
}
