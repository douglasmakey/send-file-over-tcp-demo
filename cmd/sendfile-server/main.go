package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
)

func sendFile(file *os.File, conn net.Conn) error {
	// Get file stat
	fileInfo, _ := file.Stat()

	// Send the file size
	sizeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBuf, uint64(fileInfo.Size()))
	if _, err := conn.Write(sizeBuf); err != nil {
		return err
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return errors.New("TCPConn error")
	}

	tcpF, err := tcpConn.File()
	if err != nil {
		return err
	}

	// Send the file contents
	_, err = syscall.Sendfile(int(tcpF.Fd()), int(file.Fd()), nil, int(fileInfo.Size()))
	return err
}

func main() {
	// Create the listener
	listener, err := net.Listen("tcp", ":3000")
	if err != nil {
		log.Fatal(err)
	}

	defer listener.Close()

	for {
		// Wait for a client to connect
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}

		// Send the file to the client
		go func() {
			// Open the file
			file, err := os.Open("../dummy.dat")
			if err != nil {
				return
			}
			defer file.Close()

			if err := sendFile(file, conn); err != nil {
				fmt.Println(err)
			}
			conn.Close()
		}()
	}

}
