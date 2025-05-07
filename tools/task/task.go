package main

import (
	"context"
	"path/filepath"

	taskfile "github.com/go-task/task/v3"
	"log"
	"os"
)

type options struct {
	Name string
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("name of task required")
	}
	name := os.Args[1]

	dir := filepath.Dir(".")
	entrypoint := filepath.Base("taskfile.yaml")

	e := taskfile.Executor{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,

		Dir:        dir,
		Entrypoint: entrypoint,

		Force:   false,
		Watch:   false,
		Verbose: true,
		Dry:     false,
		Summary: false,
		Color:   true,
	}
	err := e.Setup()
	if err != nil {
		log.Fatal(err)
	}

	build := taskfile.Call{
		Task: name,
	}

	err = e.Run(context.Background(), &build)
	if err != nil {
		log.Fatal(err)
	}
}
