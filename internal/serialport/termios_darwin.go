package serialport

import "golang.org/x/sys/unix"

func configure(fd int) error {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return err
	}
	makeRaw(t)
	t.Ispeed, t.Ospeed = unix.B115200, unix.B115200
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
}
