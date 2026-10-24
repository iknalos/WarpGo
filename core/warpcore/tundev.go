package warpcore

import (
	"os"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

// fdTun wraps the VpnService file descriptor. It satisfies both wireguard-go's
// tun.Device and usque's api.TunnelDevice, and counts bytes in each direction
// so the app can tell a live tunnel from one that is up but passes nothing.
type fdTun struct {
	file      *os.File
	events    chan tun.Event
	rx, tx    atomic.Uint64 // rx = written to the phone, tx = read from the phone
	closeOnce sync.Once
}

func newFdTun(fd int) (*fdTun, error) {
	// Non-blocking lets Go's poller own the fd, so Close() wakes a pending Read.
	if err := setNonblock(fd); err != nil {
		return nil, err
	}
	t := &fdTun{file: os.NewFile(uintptr(fd), "tun"), events: make(chan tun.Event, 1)}
	t.events <- tun.EventUp
	return t, nil
}

func (t *fdTun) File() *os.File           { return t.file }
func (t *fdTun) MTU() (int, error)        { return tunnelMTU, nil }
func (t *fdTun) Name() (string, error)    { return "warpgo", nil }
func (t *fdTun) Events() <-chan tun.Event { return t.events }
func (t *fdTun) BatchSize() int           { return 1 }

func (t *fdTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	n, err := t.file.Read(bufs[0][offset:])
	if err != nil {
		return 0, err
	}
	sizes[0] = n
	t.tx.Add(uint64(n))
	return 1, nil
}

func (t *fdTun) Write(bufs [][]byte, offset int) (int, error) {
	for i, b := range bufs {
		if _, err := t.file.Write(b[offset:]); err != nil {
			return i, err
		}
		t.rx.Add(uint64(len(b) - offset))
	}
	return len(bufs), nil
}

func (t *fdTun) ReadPacket(buf []byte) (int, error) {
	n, err := t.file.Read(buf)
	if err == nil {
		t.tx.Add(uint64(n))
	}
	return n, err
}

func (t *fdTun) WritePacket(pkt []byte) error {
	_, err := t.file.Write(pkt)
	if err == nil {
		t.rx.Add(uint64(len(pkt)))
	}
	return err
}

func (t *fdTun) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.events)
		err = t.file.Close()
	})
	return err
}
