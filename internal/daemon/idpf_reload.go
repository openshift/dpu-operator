package kmod

import (
        "errors"
        "fmt"
        "os/exec"
        "strings"

        "golang.org/x/sys/unix"
)

/* Rmmod attempts to remove a module via delete_module(2).
   If nonblock is true, use O_NONBLOCK only. NOTE: kernel will refuse if the driver is busy.
   If exclusive is true, the call fails if the module is in use by anyone else.
*/
func Rmmod(name string, nonblock bool, exclusive bool) error {
        flags := 0
        if nonblock {
                flags |= unix.O_NONBLOCK
        }
        if exclusive {
                flags |= unix.O_EXCL
        }
        if err := unix.DeleteModule(name, flags); err != nil {
                // Convert EBUSY to a clearer message.
                if errors.Is(err, unix.EBUSY) {
                        return fmt.Errorf("rmmod %s: module is busy. rmmod failed.", name)
                }
                if errors.Is(err, unix.ENOENT) {
                        return nil // successfully removed.
                }
                return fmt.Errorf("rmmod %s: %w", name, err)
        }
        return nil
}

/* Modprobe tries to load a module by name using modprobe along with its dependencies.
   opts can handle optional parameters like debug mode or similar.
*/
func Modprobe(name string, opts ...string) error {
        args := append([]string{name}, opts...)
        cmd := exec.Command("modprobe", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
                return fmt.Errorf("modprobe %s failed: %v; output: %s", name, err, strings.TrimSpace(string(out)))
        }
        return nil
}

/* Reload removes the driver and loads it back.
*/
func Reload(name string) error {
        // Best effort remove (non-blocking + exclusive to avoid stuck waits).
        if err := Rmmod(name, true, true); err != nil {
                return err
        }
        if err = Modprobe(name); err != nil {
                return err
        }
        return nil
}
