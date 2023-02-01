package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

func sendFile(file *os.File, conn net.Conn) error {
	// Get file stat
	fileInfo, _ := file.Stat()
	// Send the file size
	sizeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBuf, uint64(fileInfo.Size()))
	_, err := conn.Write(sizeBuf)
	if err != nil {
		return err
	}

	// Send the file contents
	buf := make([]byte, 1024)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			fmt.Println("error writing to the conn:", err)
			return err
		}
	}

	return nil
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
				log.Println(err)
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
