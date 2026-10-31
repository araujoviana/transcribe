package main

import (
	"os"
	"os/exec"
)

func newPipe() (*os.File, *os.File) {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	return r, w
}

func lookPath(bin string) (string, error) { return exec.LookPath(bin) }
