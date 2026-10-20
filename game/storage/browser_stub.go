//go:build !(js && wasm)

package storage

func (BrowserStore) Read(string) (string, bool, error)  { return "", false, ErrStorageUnavailable }
func (BrowserStore) Set(string, string) error           { return ErrStorageUnavailable }
func (BrowserStore) Delete(string) error                { return ErrStorageUnavailable }
func (BrowserStore) Keys(string, int) ([]string, error) { return nil, ErrStorageUnavailable }
