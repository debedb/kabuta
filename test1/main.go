package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	dlvCmd *exec.Cmd
	wg     sync.WaitGroup
)

func readStream(stream io.ReadCloser) {
	defer wg.Done()
	buf := make([]byte, 1024)

	prev := ""
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			out := prev + string(buf[0:n])
			prev = ""
			lines := strings.Split(out, "\n")
			for _, line := range lines[0 : len(lines)-1] {
				fmt.Println(line)
			}
			lastLine := lines[len(lines)-1]
			if strings.HasSuffix(out, "\n") {
				fmt.Println(lastLine)
			} else {
				prev = lastLine
			}
		}
		if n == 0 && err == io.EOF && dlvCmd.ProcessState != nil && dlvCmd.ProcessState.Exited() {
			fmt.Println("Finished.")
			break
		} else if err != nil && err != io.EOF {
			panic(err)
		}
	}
}

func main() {
	var err error
	wg.Add(2)
	dlvCmd = exec.Command("/usr/local/bin/dlv", "debug", "--headless", "--api-version=2", "--log", "--listen=127.0.0.1:8282", "--", "one", "two")
	dlvCmd.Dir = "/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/src/github.com/example/example/cli"
	dlvStdout, err := dlvCmd.StdoutPipe()

	if err != nil {
		panic(err)
	}
	dlvStderr, err := dlvCmd.StderrPipe()
	if err != nil {
		panic(err)
	}
	err = dlvCmd.Start()
	if err != nil {
		panic(err)
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("dlv pid: %d\n", dlvCmd.Process.Pid)
	fmt.Printf("Exited: %b\n", dlvCmd.ProcessState)
	//	jsonrpc.Dial(", address)
	go readStream(dlvStdout)
	go readStream(dlvStderr)
	wg.Wait()

}
