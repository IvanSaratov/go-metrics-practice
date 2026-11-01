package main

import (
	"os"
	. "os"
	system "os"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(1) // want "прямой вызов os.Exit запрещён в функции main пакета main"
	}

	defer os.Exit(0) // want "прямой вызов os.Exit запрещён в функции main пакета main"
	go os.Exit(1)    // want "прямой вызов os.Exit запрещён в функции main пакета main"
	(os.Exit)(0)     // want "прямой вызов os.Exit запрещён в функции main пакета main"
	system.Exit(1)   // want "прямой вызов os.Exit запрещён в функции main пакета main"
	Exit(1)          // want "прямой вызов os.Exit запрещён в функции main пакета main"

	// Дальше идут вызовы, на которые анализатор не должен ругаться.
	defer helper()
	go helper()
	helper()
	_ = os.Getpid()

	exit := os.Exit
	exit(0)

	func() {
		os.Exit(0)
	}()
	defer func() {
		os.Exit(0)
	}()
	go func() {
		os.Exit(0)
	}()

	Exit := func(int) {}
	Exit(0)

	os := process{}
	os.Exit(0)
	os.main()

	fn := func() {}
	fn()
}

func helper() {
	os.Exit(1)
}

type process struct{}

func (process) Exit(int) {}

func (process) main() {
	os.Exit(1)
}
