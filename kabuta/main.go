// Copyright (c) 2016 Gregory Golberg (grisha@alum.mit.edu)
//
// This software is licensed under MIT License:
//
// https://opensource.org/licenses/MIT

package main

// TODO
//

import (
	"github.com/debedb/kabuta"
)

func main() {
	err := kabuta.Run()
	if err != nil {
		panic(err)
	}
}
