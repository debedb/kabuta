package main

import (
	"fmt"
	"os"
)

func main() {
	s := "hello, world\n"
	fmt.Println(s)
	fmt.Printf("Args: %v\n", os.Args)
}
