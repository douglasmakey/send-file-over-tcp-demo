# send-file-over-tcp-demo

As I experiment with Raspberry Pi and other devices in my network, I have created a small network application to aid in device discovery using multicast, data collection, and other functions.

One key feature of this application is the ability to download various data and metrics from some plugins weekly. With file sizes ranging from 200 MB to 250 MB after applying some compression, it's essential to carefully consider some approaches for sending these files over TCP using Go.

In this article, we'll explore some approaches and tips for sending large files over TCP in linux using Go, taking into account the constraints of small devices and the importance of efficient and reliable file transmission.

## Naive approach

```go
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

	// Send the file contents by chunks
	buf := make([]byte, 1024)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			fmt.Println("error writing to the conn:", err)
			break
		}
	}

	return nil
}
```

Although this code appears straightforward, it has a significant drawback regarding efficiency. The code moves data in a loop from the kernel buffer for the source to a buffer in user space and then immediately copies it from that buffer to the kernel buffer for the destination. This double copying of data results in a loss of performance as the buffer serves only as a temporary holding place.

While increasing the `buf` size to minimize the number of system calls might seem like a viable solution, it actually results in an increase in memory usage, making it an inefficient approach for tiny devices.

Moreover, the double copying of data also increases memory usage, as both the source and destination buffers must be allocated and maintained in memory. This can strain the system's resources, particularly when transferring large files and the devices are small.

![naive-approach](https://www.kungfudev.com/img/naive-approach.png)

The above diagram provides a simplified illustration of data flow when sending files over TCP. Using the previous approach, it's important to note that the data is copied four times before the process is complete:

1.  From the `disk` to the `read buffer` in the kernel space.
2.  From the `read buffer` in the kernel space to the `app buffer` in the user space.
3.  From the `app buffer` in the user space to the `socket buffer` in the kernel space.
4.  Finally, from the `socket buffer` in the kernel space to the Network Interface Controller (NIC).

This highlights the inefficiency of copying data multiple times, that's without mention the multiple context switches between user mode and kernel mode.

The data is copied from the disk to the read buffer in kernel space when a `read()` call is issued, and the copy is performed by direct memory access (DMA). This results in a context switch from user mode to kernel mode. The data is then copied from the read buffer to the app buffer by the CPU, which requires another context switch from kernel to user mode.

When a `write/send()` call is made, another context switch from user mode to kernel mode occurs, and the data is copied from the app buffer to a socket buffer in kernel space by the CPU. Then, a fourth context switch occurs as the `write/send()` call returns. The DMA engine then passes the data to the protocol engine asynchronously.

> What is DMA?
>
> DMA stands for Direct Memory Access. It's a technology that allows peripheral devices to access computer memory directly, without needing the CPU, to speed up data transfer. In this way, the CPU is freed from performing the data transfer itself, allowing it to perform other tasks and making the system more efficient.
> https://en.wikipedia.org/wiki/Direct_memory_access

To optimize the file transfer process, we have to minimize the number of buffer copies and context switches and reduce the overhead of moving data from one place to another.

## Using a specialized syscall 'sendfile'

Golang provides access to low-level operating system functionality through the `syscall` package, which contains an interface to various system primitives.

```go
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
```

> sendfile() copies data between one file descriptor and another. Because this copying is done within the kernel, sendfile() is more efficient than the combination of read(2) and write(2), which would require transferring data to and from user space.
> https://man7.org/linux/man-pages/man2/sendfile.2.html

The `sendfile` syscall is more efficient in transferring data than standard read and write methods. By bypassing the app buffer, the data moves directly from the read buffer to the socket buffer, reducing the number of data copies and context switches and improving performance. Furthermore, the process could requires less CPU intervention, allowing quicker data transfer and freeing up CPU for other tasks.

The `sendfile` syscall is known as a "zero-copy" method because it transfers data from one file descriptor to another without the need for an intermediate data copy in user-space memory.

Of course this "zero-copy" is from a user-mode application point of view.


![sendfile without sg](https://www.kungfudev.com/img/sendfile-not-sg-approach.png)

This scenario has two DMA copies + one CPU copy, and two context switches.

The `sendfile` syscall becomes even more efficient when the NIC supports Scatter/Gather. With SG, the syscall can directly transfer the data from the read buffer to the NIC, making the transfer a zero-copy operation that reduces the CPU load and enhances performance.

> Gather refers to the ability of a Network Interface Card (NIC) to receive data from multiple memory locations and combine it into a single data buffer before transmitting it over the network. A NIC's scatter/gather feature is used to increase the efficiency of data transfer by reducing the number of memory copies required to transmit the data. Instead of copying the data into a single buffer, the NIC can gather data from multiple buffers into a single buffer, reducing the CPU load and increasing the transfer's performance.
> https://en.wikipedia.org/wiki/Gather/scatter_(vector_addressing)

**Nic with gather supports**

![sendfile with sg](https://www.kungfudev.com/img/sendfile-sg-approach.png)

This scenario has just two DMA copies and two context switches.

Therefore, reducing the number of buffer copies not only improves performance but also reduces memory usage, making the file transfer process more efficient and scalable.

Note that the illustrations and scenarios provided are highly simplified and don't fully represent the complexity of these processes. However, the aim was to present the information in a straightforward and easy-to-understand manner.

## Why is "io.Copy" frequently recommended in Go?

```go
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
	_, err = io.Copy(conn, file)
	return err
}
```

The recommendation to use the `io.Copy` function in Go is due to its simplicity and efficiency. This function offers a streamlined way to copy data from an io.Reader to an io.Writer, managing buffering and chunking data to minimize memory usage and reduce syscalls. Additionally, io.Copy handles any potential errors during the copy process, making it a convenient and dependable option for data copying in Go.

The benefits of using io.Copy in Go go beyond its 32k buffer management and optimization [src](https://cs.opensource.google/go/go/+/refs/tags/go1.19.5:src/io/io.go;l=424).

```go
func copyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	...
	if wt, ok := src.(WriterTo); ok {
		return wt.WriteTo(dst)
	}

	if rt, ok := dst.(ReaderFrom); ok {
		return rt.ReadFrom(src)
	}
	...
}
```

When the destination satifies the `ReadFrom` interface, io.Copy utilizes this by calling `ReadFrom` to handle the copy process. For example, when `dst` is a `TCPConn`, `io.Copy` will call the underlying function to complete the copy [src](https://cs.opensource.google/go/go/+/refs/tags/go1.19.5:src/net/tcpsock_posix.go;drc=007d8f4db1f890f0d34018bb418bdc90ad4a8c35;l=47).

```go
func (c *TCPConn) readFrom(r io.Reader) (int64, error) {
	if n, err, handled := splice(c.fd, r); handled {
		return n, err
	}
	if n, err, handled := sendFile(c.fd, r); handled {
		return n, err
	}
	return genericReadFrom(c, r)
}
```

As you can see, when sending a file over a TCP connection, `io.copy` utilizes the `sendfile` syscall for efficient data transfer.

By running the program and using the `strace` tool to log all system calls, you can observe the use of the `sendfile` syscall in action:

```txt
...
[pid 67436] accept4(3,  <unfinished ...>
...
[pid 67440] epoll_pwait(5,  <unfinished ...>
[pid 67436] sendfile(4, 9, NULL, 4194304) = 143352
...
```

As observed in the implementation of `ReadFrom`, `io.Copy` not only attempts to use `sendfile`, but also the `splice` syscall, another useful system call for efficiently transferring data through pipes.

In addition, when the source satifies the `WriteTo` method, `io.Copy` will utilize it for the copy, avoiding any allocations and reducing the need for extra copying. This is why experts recommend using `io.Copy` whenever possible for copying or transferring data.


### Possible tips for Linux.

I also try to improve performance on Linux systems for generic scenarios by increasing the MTU (Maximum Transmission Unit) size of the network interfaces and changing the TCP buffer size.

The Linux kernel parameters `tcp_wmem` and `tcp_rmem` control the transmit and receive buffer size for TCP connections, respectively. These parameters can be used to optimize the performance of TCP sockets.

`tcp_wmem` determines the write buffer size for each socket, storing outgoing data before it is sent to the network. Larger buffers increase the amount of data sent at once, improving network efficiency.

`tcp_rmem` sets the read buffer size for each socket, holding incoming data before the application processes it. This helps prevent network congestion and enhances efficiency.

Increasing both values will demand more memory usage.

[Read more.](https://www.ibm.com/docs/en/linux-on-systems?topic=tuning-tcpip-ipv4-settings)


```bash
# See current tcp buffer values
$ sysctl net.ipv4.tcp_wmem
net.ipv4.tcp_wmem = 4096 16384 4194304

# Change the values
$ sysctl -w net.ipv4.tcp_wmem="X X X"

# Change MTU
$ ifconfig <Interface_name> mtu <mtu_size> up
```


For me, these optimizations failed to deliver a substantial improvement due to certain constraints, such as the limitations of some devices, local network, etc.


### In conclusion.

The article discussed ways to send large files over TCP in Linux using Go, considering the constraints of small devices and the importance of efficient and reliable file transmission. The naive approach of copying data multiple times was deemed inefficient and increased memory usage, causing strain on the system's resources. An alternative approach was presented, using the specialized syscall 'sendfile' and, more importantly, `io.Copy` which use `sendfile` under the hood for this scenario to minimize the number of buffer copies and context switches and reduce overhead to achieve a more efficient file transfer.

Thank you for taking the time to read this article. I hope it provided some helpful information. I constantly work to improve my understanding and knowledge, so I appreciate your feedback or corrections. Thank you again for your time and consideration.

[Repo](https://github.com/douglasmakey/send-file-over-tcp-demo)

### Generate a demo file for testing

```sh
dd if=/dev/urandom of=dummy.dat bs=1M count=230 # This generate a file with size of 230MB aprox with random data!
```
