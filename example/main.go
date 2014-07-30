package main

import (
	"log"
	"os"
	"path"

	"github.com/vmware/gotftp"
)

type Handler struct {
	Path string
}

func (h Handler) ReadFile(c gotftp.Conn, filename string) (gotftp.ReadCloser, error) {
	log.Printf("Request from %s to read %s", c.RemoteAddr(), filename)
	return os.OpenFile(path.Join(h.Path, filename), os.O_RDONLY, 0)
}

func (h Handler) WriteFile(c gotftp.Conn, filename string) (gotftp.WriteCloser, error) {
	log.Printf("Request from %s to write %s", c.RemoteAddr(), filename)
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
