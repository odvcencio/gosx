//go:build !(js && wasm)

package socket

// Socket is the browser transport. Native applications can use hub/client.
type Socket struct{}

func Dial(string, Callbacks) (*Socket, error) { return nil, ErrUnsupported }
func (*Socket) ReadyState() State             { return Closed }
func (*Socket) BufferedAmount() int           { return 0 }
func (*Socket) SendText(string) error         { return ErrUnsupported }
func (*Socket) SendBinary([]byte) error       { return ErrUnsupported }
func (*Socket) Close(int, string) error       { return nil }
func (*Socket) Dispose()                      {}
