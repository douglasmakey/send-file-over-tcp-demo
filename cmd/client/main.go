package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
)

func receiveFile(path string, conn net.Conn) error {
	// Create the file
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	// Get the file size
	sizeBuf := make([]byte, 8)
	if _, err := conn.Read(sizeBuf); err != nil {
		return err
	}

	fileSize := binary.LittleEndian.Uint64(sizeBuf)
	// Receive the file contents
	_, err = io.CopyN(file, conn, int64(fileSize))
	return err
}

func main() {
	// Connect to the server
	conn, err := net.Dial("tcp", "x.x.x.x:3000")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Receive the file from the server
	err = receiveFile("local.dat", conn)
	if err != nil {
		log.Fatal(err)
	}
}
