package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	ch    chan string
	wg    sync.WaitGroup
	mutex *sync.Mutex
)

func goroutineChan(id int) {
	for {
		select {
		case msg := <-ch:
			fmt.Printf("goroutineChan %d received %s on the channel", id, msg)
		default:
		}
		msg := fmt.Sprintf("Hello from %d", id)
		select {
		case ch <- msg:
			fmt.Printf("goroutineChan %d sent message %s", msg)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func goroutineLock(id int) {
	for {
		mutex.Lock()
		fmt.Printf("goroutineLock %d says Hi", id)
		time.Sleep(1 * time.Nanosecond)
		mutex.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	s := "hello, world\n"
	fmt.Println(s)
	fmt.Printf("Args: %v\n", os.Args)
	ch = make(chan string)
	mutex = &sync.Mutex{}
	wg.Add(4)
	go goroutineChan(1)
	go goroutineChan(2)
	go goroutineLock(3)
	go goroutineLock(4)
	wg.Wait()
}
