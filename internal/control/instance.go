package control

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/thgossler/mdv/internal/core"
)

const Env = "MDV_API"

type Instance struct {
	Server *Server
	Path   string
}

func StartInstance(mode string, handler Handler, quit func()) (*Instance, error) {
	base, err := core.ConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, "api")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := secureDirectory(dir); err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(secret)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	name := make([]byte, 12)
	if _, err := rand.Read(name); err != nil {
		listener.Close()
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s.json", os.Getpid(), hex.EncodeToString(name)))
	tempPath := path + ".tmp"
	file, err := privateFile(tempPath)
	if err != nil {
		listener.Close()
		return nil, err
	}
	record := map[string]any{"version": 1, "address": listener.Addr().String(), "token": token, "pid": os.Getpid(), "mode": mode}
	err = json.NewEncoder(file).Encode(record)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tempPath)
		listener.Close()
		return nil, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		listener.Close()
		return nil, err
	}
	return &Instance{Server: Start(listener, token, handler, quit), Path: path}, nil
}

func (instance *Instance) Close() {
	instance.Server.Close()
	os.Remove(instance.Path)
}
