package main

import (
	"log"
	"net"
	"os"
	"path"

	"github.com/vmware/gotftp"
)

type Handler struct {
	Path string
}

func (h Handler) ReadFile(peer net.Addr, filename string) (gotftp.ReadCloser, error) {
	log.Printf("Request from %s to read %s", peer, filename)
	return os.OpenFile(path.Join(h.Path, filename), os.O_RDONLY, 0)
}

func (h Handler) WriteFile(peer net.Addr, filename string) (gotftp.WriteCloser, error) {
	log.Printf("Request from %s to write %s", peer, filename)
	return os.OpenFile(path.Join(h.Path, filename), os.O_WRONLY, 0644)
}

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	h := Handler{Path: pwd}
	err = gotftp.ListenAndServe(h)
	panic(err)
}
