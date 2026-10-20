package applog_test

import (
	"fmt"
	"os"

	"m31labs.dev/gosx/desktop/applog"
)

func ExampleOpen() {
	dir, err := os.MkdirTemp("", "applog-example-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)

	log, err := applog.Open(applog.Options{Dir: dir, Name: "studio"})
	if err != nil {
		fmt.Println(err)
		return
	}
	if _, err := log.Write([]byte("application started\n")); err != nil {
		fmt.Println(err)
		log.Close()
		return
	}
	if err := log.Close(); err != nil {
		fmt.Println(err)
		return
	}
	data, err := os.ReadFile(log.Path())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s", data)
	// Output: application started
}
